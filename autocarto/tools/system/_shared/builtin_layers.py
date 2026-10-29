"""Local layer registrations: metadata probing, read-only loading and previews."""
from pathlib import Path
import hashlib
import math
import os
import statistics
import yaml
from qgis.core import (QgsCoordinateReferenceSystem, QgsCoordinateTransform, QgsDistanceArea,
    QgsFeature, QgsFeatureRequest, QgsGeometry, QgsMapSettings, QgsMapRendererParallelJob,
    QgsProject, QgsRectangle, QgsPointXY, QgsVectorLayer, QgsRasterLayer, QgsVectorFileWriter, QgsWkbTypes)
from qgis.PyQt.QtCore import QSize
from qgis.PyQt.QtGui import QColor
from autocarto.core.paths import get_plugin_root, get_autocarto_home, resolve_project_path


def assets_root():
    return resolve_project_path(os.environ['AUTOCARTO_ASSETS_ROOT']) if os.environ.get('AUTOCARTO_ASSETS_ROOT') else get_plugin_root()


def data_digest(path):
    path = Path(path)
    files = [path]
    if path.suffix.lower() == '.shp':
        files += [path.with_suffix(ext) for ext in ('.shx', '.dbf', '.prj', '.cpg') if path.with_suffix(ext).is_file()]
    digest = hashlib.sha256()
    for item in sorted(files):
        digest.update(item.name.encode() + b'\0')
        with item.open('rb') as stream:
            for chunk in iter(lambda: stream.read(1024 * 1024), b''):
                digest.update(chunk)
    return 'sha256:' + digest.hexdigest()


def registrations():
    result = []
    for root in (assets_root() / 'data/layers/system', get_autocarto_home() / 'layers'):
        for path in sorted(root.glob('*/*/layer.yaml')):
            manifest = yaml.safe_load(path.read_text())
            manifest['_directory'] = str(path.parent)
            result.append(manifest)
    return result


def resolve_registration(name):
    items = registrations()
    exact = [item for item in items if item['name'] == name or item['id'] == name]
    found = exact or [item for item in items if name in item.get('aliases', [])]
    if len(found) != 1:
        raise ValueError('内置图层不存在或别名有歧义：' + name)
    item = found[0]
    path = Path(item['file'])
    if not path.is_absolute():
        path = Path(item['_directory']) / path
    if data_digest(path) != item['digest']:
        raise ValueError('内置图层原数据已变化，请在资源库重新确认：' + name)
    return item, path


def open_layer(path, name='', sublayer='', crs=''):
    source = str(path) + ('|layername=' + sublayer if sublayer else '')
    raster = Path(path).suffix.lower() in ('.tif', '.tiff', '.vrt', '.img', '.asc')
    layer = QgsRasterLayer(source, name or Path(path).stem) if raster else QgsVectorLayer(source, name or Path(path).stem, 'ogr')
    if not layer.isValid():
        raise ValueError('无法读取图层：' + source)
    if crs:
        actual = QgsCoordinateReferenceSystem(crs)
        if not actual.isValid():
            raise ValueError('实际坐标系无效：' + crs)
        layer.setCrs(actual)
    if isinstance(layer, QgsVectorLayer):
        layer.setReadOnly(True)
    return layer


def apply_style(layer, name, label_field=''):
    if name:
        from autocarto.tools.system._shared.resource_handle import require_handle, handle_entry_path
        handle = require_handle(name, 'style', formats=('qml',))
        message, ok = layer.loadNamedStyle(str(handle_entry_path(handle)))
        if not ok:
            raise ValueError(message)
        from autocarto.tools.system._shared.style_labeling import restore_qml_font_families
        restore_qml_font_families(layer)
    if isinstance(layer, QgsVectorLayer):
        if label_field and label_field != '无':
            if label_field not in layer.fields().names():
                raise ValueError('标注字段不存在：' + label_field)
            from autocarto.tools.system._shared.style_labeling import _override_label_field
            _override_label_field(layer, label_field)
        else:
            layer.setLabelsEnabled(False)


def render_layer(path, output, params):
    layer = open_layer(path, params.get('name', ''), params.get('sublayer', ''), params.get('crs', ''))
    apply_style(layer, params.get('style', ''), params.get('label_field', ''))
    settings = QgsMapSettings()
    settings.setLayers([layer])
    settings.setDestinationCrs(layer.crs())
    extent = layer.extent()
    if extent.isEmpty():
        extent.grow(0.01)
    extent.scale(1.08)
    settings.setExtent(extent)
    settings.setOutputSize(QSize(960, 640))
    settings.setBackgroundColor(QColor('#f8fafc'))
    job = QgsMapRendererParallelJob(settings)
    job.start()
    job.waitForFinished()
    Path(output).parent.mkdir(parents=True, exist_ok=True)
    if not job.renderedImage().save(str(output), 'PNG'):
        raise ValueError('图层预览保存失败')
    return {'width': 960, 'height': 640, 'mode': 'layer'}


def inspect_layer(params):
    if params.get('layer_id'):
        source = QgsProject.instance().mapLayer(params['layer_id'])
        if source is None:
            raise ValueError('工程图层不存在：' + params['layer_id'])
        layer = source.clone()
    else:
        layer = open_layer(Path(params['path']), sublayer=params.get('sublayer', ''))
    vector = isinstance(layer, QgsVectorLayer)
    declared = layer.crs().authid() or layer.crs().toWkt()
    crs = params.get('crs') or declared
    if params.get('crs'):
        actual = QgsCoordinateReferenceSystem(crs)
        if not actual.isValid():
            raise ValueError('实际坐标系无效：' + crs)
        layer.setCrs(actual)
    messages = []
    needs = not layer.crs().isValid() or not layer.crs().authid()
    world = QgsCoordinateReferenceSystem('EPSG:4326')
    provinces = QgsVectorLayer(str(assets_root() / 'tools/system/admin-boundaries/assets/中国行政区边界/data/china_province.geojson'), '省', 'ogr')
    province_features = list(provinces.getFeatures())
    # Compare named provincial polygons when identity is present; otherwise only
    # assess overlap. A contained small layer cannot prove its datum is correct.
    def assess(candidate):
        transform = QgsCoordinateTransform(candidate, world, QgsProject.instance())
        box = transform.transformBoundingBox(layer.extent())
        values = [box.xMinimum(), box.yMinimum(), box.xMaximum(), box.yMaximum()]
        if not all(math.isfinite(v) for v in values) or not (-180 <= values[0] <= values[2] <= 180 and -90 <= values[1] <= values[3] <= 90):
            return None
        intersects = [f['name'] for f in province_features if f.geometry().intersects(box)]
        score = 0.0 if intersects else 1e12
        if vector and layer.geometryType() == QgsWkbTypes.PolygonGeometry:
            name_field = next((x for x in ('name', 'NAME') if x in layer.fields().names()), None)
            if name_field:
                errors = []
                for feature in layer.getFeatures():
                    target = next((p for p in province_features if p['name'] == feature[name_field]), None)
                    if target:
                        vertices = list(feature.geometry().vertices())
                        source_box = layer.extent()
                        boundary = target.geometry().convertToType(QgsWkbTypes.LineGeometry)
                        # Cropped edges are not administrative boundaries. Compare
                        # the remaining sampled vertices with the matching province.
                        for vertex in vertices[::max(1, len(vertices)//40)]:
                            if any(abs(value-edge)<1e-7 for value,edge in ((vertex.x(),source_box.xMinimum()),(vertex.x(),source_box.xMaximum()),(vertex.y(),source_box.yMinimum()),(vertex.y(),source_box.yMaximum()))):
                                continue
                            point = QgsGeometry.fromPointXY(transform.transform(QgsPointXY(vertex)))
                            errors.append(point.distance(boundary) * 111000)
                if errors:
                    score = statistics.median(errors)
        return values, intersects, score
    evaluated = None
    if layer.crs().isValid():
        try:
            evaluated = assess(layer.crs())
        except Exception as exc:
            messages.append('声明坐标系无法转换范围：' + str(exc))
    if evaluated is None or evaluated[2] > 1000:
        needs = True
    suggested = ''
    if needs:
        choices = []
        for epsg in [4326,4490,4214,4610] + list(range(4513,4555)):
            try:
                candidate = assess(QgsCoordinateReferenceSystem('EPSG:' + str(epsg)))
                if candidate:
                    choices.append((candidate[2], epsg, candidate))
            except Exception:
                # Rejected CRS candidates are expected during coordinate probing.
                continue
        if choices:
            choices.sort(key=lambda value: value[0])
            score, epsg, best = choices[0]
            if len(choices)==1 or choices[1][0]-score > 1:
                suggested = 'EPSG:' + str(epsg)
                messages.append('坐标系需要确认；比对建议 ' + suggested + '，不能仅凭范围证明坐标系一致')
            else:
                messages.append('多个坐标系候选的范围均匹配，无法唯一判定；请确认数据的实际坐标系')
        else:
            messages.append('无法识别实际坐标系，请填写实际坐标系后确认')
    scale = {}
    if vector and layer.geometryType() != QgsWkbTypes.PointGeometry and layer.crs().isValid():
        meter = QgsDistanceArea(); meter.setSourceCrs(layer.crs(), QgsProject.instance().transformContext()); meter.setEllipsoid('WGS84')
        distances = []
        for feature in layer.getFeatures(QgsFeatureRequest().setLimit(1000)):
            previous = None
            for vertex in feature.geometry().vertices():
                if previous is not None:
                    distance = meter.measureLine(QgsPointXY(previous), QgsPointXY(vertex))
                    if distance > 0 and math.isfinite(distance):
                        distances.append(distance)
                previous = vertex
                if len(distances) >= 20000: break
            if len(distances) >= 20000: break
        if distances:
            estimate = statistics.median(distances) / 0.0005
            scale = {'best_from': round(estimate * .5), 'best_to': round(estimate * 4)}
    return {'declared_crs': declared, 'crs': crs, 'suggested_crs': suggested,
            'needs_confirmation': needs, 'messages': messages,
            'geometry': ('point','line','polygon')[layer.geometryType()] if vector and layer.geometryType() <= 2 else 'raster',
            'feature_count': layer.featureCount() if vector else 0,
            'available_fields': layer.fields().names() if vector else [],
            'coverage': {'bbox': evaluated[0], 'regions': evaluated[1]} if evaluated else {}, 'scale': scale}


def layer_inspect_handler(params):
    try:
        from autocarto.tools.system._shared.layout_runtime import run_in_main_thread
        return {'success': True, 'data': run_in_main_thread(lambda: inspect_layer(params)), 'info': '图层元数据已读取'}
    except Exception as exc:
        return {'success': False, 'info': str(exc)}


def layer_load_builtin_handler(params):
    try:
        from autocarto.tools.system._shared.layout_runtime import run_in_main_thread
        def load():
            item, path = resolve_registration(params['name'])
            from autocarto.tools.system._shared.output_manager import sync_session_context_from_params, resolve_output_path
            sync_session_context_from_params(params)
            layer = open_layer(path, params.get('layer_name') or item['name'], item.get('sublayer',''), item['crs'])
            vector = isinstance(layer, QgsVectorLayer)
            extent = params.get('extent')
            clip = params.get('clip') in (True, 'true') or (vector and layer.featureCount() > 200000) or path.stat().st_size > 200 << 20
            if clip:
                if not extent or len(extent) != 4:
                    raise ValueError('此图层需要裁剪，请提供 EPSG:4326 范围')
                import processing
                output = resolve_output_path(None, item['id'].replace('.','_') + ('.gpkg' if vector else '.tif'), 'intermediate')
                transform = QgsCoordinateTransform(QgsCoordinateReferenceSystem('EPSG:4326'), layer.crs(), QgsProject.instance())
                rectangle = transform.transformBoundingBox(QgsRectangle(*extent))
                if vector:
                    processing.run('native:extractbyextent', {'INPUT': layer, 'EXTENT': rectangle, 'CLIP': True, 'OUTPUT': output})
                else:
                    processing.run('gdal:cliprasterbyextent', {'INPUT': layer, 'PROJWIN': rectangle, 'OVERCRS': False, 'DATA_TYPE': 0, 'OUTPUT': output})
                layer = open_layer(output, params.get('layer_name') or item['name'])
            apply_style(layer, params.get('style') or item.get('default_style',''), item.get('label_field',''))
            # Repeated requests for the same registration reuse its project layer.
            project = QgsProject.instance()
            prior = next((existing for existing in project.mapLayers().values() if existing.customProperty('autocarto/builtin_id') == item['id'] and existing.source() == layer.source()), None)
            if prior:
                prior.setCustomProperty('autocarto/label_field', item.get('label_field', ''))
                from autocarto.tools.system._shared.data_loading_impl import _load_data_layer
                return _load_data_layer({'path': prior.source(), 'layer_name': params.get('layer_name'), 'show': params.get('show')}, 'vector' if vector else 'raster')
            layer.setCustomProperty('autocarto/builtin_id', item['id'])
            layer.setCustomProperty('autocarto/label_field', item.get('label_field', ''))
            layer.setCustomProperty('autocarto/default_style', item.get('default_style', ''))
            source = {'name': item['name'], 'pack': item['pack'], **item.get('source',{}), 'compliance': item.get('compliance','')}
            import json
            layer.setCustomProperty('autocarto/source', json.dumps(source, ensure_ascii=False))
            owner = params.get('_autocarto_output_owner') or {}
            if owner.get('session_id'):
                layer.setCustomProperty('autocarto/output_owner', json.dumps([owner['session_id'], owner.get('flow_run_id', ''), owner.get('step_id', '')]))
            project.addMapLayer(layer, False)
            from autocarto.tools.system._shared.data_loading_impl import _load_data_layer
            result = _load_data_layer({'layer_id': layer.id(), 'show': params.get('show')}, 'vector' if vector else 'raster')
            result['data'].update(provenance=source, clipped=clip, reused=False)
            return result
        return run_in_main_thread(load)
    except Exception as exc:
        return {'success': False, 'info': str(exc), 'error_type': 'builtin_layer_failed'}


def layer_remove_created_handler(params):
    """Remove only the exact IDs owned by the current execution attempt."""
    from autocarto.tools.system._shared.layout_runtime import run_in_main_thread
    ids = params.get('layer_ids')
    if not isinstance(ids, list) or not all(isinstance(value, str) for value in ids):
        return {'success': False, 'info': 'layer_ids 必须是图层 ID 列表'}
    keep_layouts = params.get('keep_layouts') or []
    if not isinstance(keep_layouts, list) or not all(isinstance(value, str) for value in keep_layouts):
        return {'success': False, 'info': 'keep_layouts 必须是版面名称列表'}
    def remove():
        from qgis.core import QgsLayoutItemMap, QgsLayoutItemLegend
        project = QgsProject.instance()
        retained = set()
        for name in keep_layouts:
            layout = project.layoutManager().layoutByName(name)
            if layout is None:
                continue
            for item in layout.items():
                if isinstance(item, QgsLayoutItemMap):
                    retained.update(layer.id() for layer in item.layers())
                elif isinstance(item, QgsLayoutItemLegend):
                    retained.update(node.layerId() for node in item.model().rootGroup().findLayers())
        candidates = [value for value in dict.fromkeys(ids) if project.mapLayer(value) is not None]
        removed = [value for value in candidates if value not in retained]
        project.removeMapLayers(removed)
        return {'success': True, 'data': {'removed_ids': removed, 'retained_ids': [value for value in candidates if value in retained]}, 'info': '已清理本次创建的图层'}
    return run_in_main_thread(remove)
