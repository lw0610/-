"""Native symbol legends and real-data previews preserve the user's project."""
from pathlib import Path
import pytest
from autocarto.tests.test_style_set_qgis import style_qgis_runtime, style_project, _layer, _state


def _preview(tmp_path, path, **params):
    from autocarto.tools.system._shared.resource_preview_impl import resource_preview_handler
    output = tmp_path / ('layer.png' if params.get('layer_id') else 'legend.png')
    result = resource_preview_handler(dict(resource_path=str(path), resource_type='style', output_path=str(output), **params))
    return result, output


def _save(layer, tmp_path):
    path = tmp_path / 'style.qml'
    assert layer.saveNamedStyle(str(path))[1]
    return path


def _assert_png(output, width):
    from qgis.PyQt.QtGui import QImage
    image = QImage(str(output))
    assert not image.isNull() and image.width() == width
    assert any(value != 255 for value in image.constBits().asstring(image.sizeInBytes()))


def test_category_legend_keeps_all_categories_without_fake_attribute_values(style_project, tmp_path):
    from qgis.core import QgsCategorizedSymbolRenderer, QgsRendererCategory, QgsMarkerSymbol
    layer = _layer('Point', '分类')
    symbols = [QgsRendererCategory('a', QgsMarkerSymbol.createSimple({'color':'#ee0000'}), '甲类别'),
               QgsRendererCategory('b', QgsMarkerSymbol.createSimple({'color':'#0000ee'}), '乙类别')]
    layer.setRenderer(QgsCategorizedSymbolRenderer('name', symbols))
    result, output = _preview(tmp_path, _save(layer, tmp_path))
    assert result['success'], result
    assert result['data']['labels'] == ['甲类别', '乙类别']
    assert result['data']['legend_count'] == 2 and result['data']['required_fields'] == ['name']
    _assert_png(output, 960)
    from qgis.PyQt.QtGui import QImage
    image = QImage(str(output))
    colors = [image.pixelColor(x,y) for x in range(40,290,4) for y in range(85,320,4)]
    assert any(c.red()>150 and c.blue()<100 for c in colors)
    assert any(c.blue()>150 and c.red()<100 for c in colors)


def test_graduated_and_rule_legends_expose_each_label_and_scale(style_project, tmp_path):
    from qgis.core import QgsGraduatedSymbolRenderer, QgsRendererRange, QgsFillSymbol, QgsRuleBasedRenderer
    layer = _layer('Polygon', '分级')
    layer.setRenderer(QgsGraduatedSymbolRenderer('population', [
        QgsRendererRange(0, 10, QgsFillSymbol.createSimple({'color':'#ffeeaa'}), '0–10'),
        QgsRendererRange(10, 20, QgsFillSymbol.createSimple({'color':'#883300'}), '10–20')]))
    result, _ = _preview(tmp_path, _save(layer, tmp_path))
    assert result['success'] and result['data']['labels']==['0–10','10–20'], result
    root = QgsRuleBasedRenderer.Rule(None)
    for label, condition in [('甲规则','"name" = \'a\''),('乙规则','"name" = \'b\'')]:
        root.appendChild(QgsRuleBasedRenderer.Rule(QgsFillSymbol.createSimple({'color':'#cc8866'}), 1000, 50000, condition, label))
    layer.setRenderer(QgsRuleBasedRenderer(root))
    result, output = _preview(tmp_path, _save(layer, tmp_path))
    assert result['success'] and result['data']['labels']==['甲规则','乙规则'], result
    _assert_png(output, 960)


def test_real_layer_preview_changes_pixels_without_mutating_source(style_project, tmp_path):
    from qgis.core import QgsProject, QgsMarkerSymbol, QgsSingleSymbolRenderer
    from qgis.PyQt.QtGui import QImage
    project, _ = style_project
    layer = _layer('Point','我的数据')
    layer.selectAll()
    original = _state(layer)
    style_layer = layer.clone()
    style_layer.setRenderer(QgsSingleSymbolRenderer(QgsMarkerSymbol.createSimple({'name':'square','color':'#ee0000','size':'10'})))
    path = _save(style_layer, tmp_path)
    before_ids, before_selection = set(project.mapLayers()), layer.selectedFeatureIds()
    project.setDirty(False)
    result, output = _preview(tmp_path, path, layer_id=layer.id())
    assert result['success'] and result['data']['mode']=='layer', result
    _assert_png(output, 1440)
    image = QImage(str(output))
    assert image.pixelColor(image.width()//2,image.height()//2).red() > 200
    assert image.pixelColor(image.width()//2,image.height()//2).green() < 100
    assert set(project.mapLayers()) == before_ids and _state(layer) == original
    assert layer.selectedFeatureIds() == before_selection and not project.isDirty()


def test_layer_preview_missing_fields_and_geometry_reject_without_mutation(style_project, tmp_path):
    from qgis.core import QgsCategorizedSymbolRenderer, QgsRendererCategory, QgsMarkerSymbol
    project, _ = style_project
    source = _layer('Point', '用户点')
    style_layer = source.clone()
    style_layer.setRenderer(QgsCategorizedSymbolRenderer('missing_field', [QgsRendererCategory('x',QgsMarkerSymbol(),'分类')]))
    path = _save(style_layer, tmp_path)
    project.setDirty(False)
    before = _state(source)
    result, output = _preview(tmp_path, path, layer_id=source.id())
    assert not result['success'] and 'missing_field' in result['info'], result
    assert not output.exists() and _state(source)==before and not project.isDirty()
    line = _layer('LineString','用户线')
    project.setDirty(False)
    result, _ = _preview(tmp_path, path, layer_id=line.id())
    assert not result['success'] and '几何类型不一致' in result['info'], result
    assert not project.isDirty()


def test_label_expression_requires_real_fields_and_keeps_expression(style_project, tmp_path):
    from qgis.core import QgsVectorLayerSimpleLabeling, QgsPalLayerSettings
    source = _layer('Point','地名')
    styled = source.clone()
    labels = QgsPalLayerSettings(); labels.fieldName='upper("not_present")'; labels.isExpression=True
    styled.setLabeling(QgsVectorLayerSimpleLabeling(labels)); styled.setLabelsEnabled(True)
    result, _ = _preview(tmp_path, _save(styled,tmp_path), layer_id=source.id())
    assert not result['success'] and 'not_present' in result['info'], result
    labels.fieldName='upper("name")'
    styled.setLabeling(QgsVectorLayerSimpleLabeling(labels))
    result, output = _preview(tmp_path, _save(styled,tmp_path), layer_id=source.id())
    assert result['success'], result
    _assert_png(output,1440)


def test_raster_legend_has_real_color_stops_and_rgb_requires_layer(style_project, tmp_path):
    path=tmp_path/'palette.qml'
    path.write_text('<qgis><pipe><rasterrenderer type="singlebandpseudocolor"><rastershader><colorrampshader><item value="1" label="低" color="#00ff00" alpha="255"/><item value="2" label="高" color="#ff0000" alpha="255"/></colorrampshader></rastershader></rasterrenderer></pipe></qgis>')
    result, output = _preview(tmp_path,path)
    assert result['success'] and result['data']['labels']==['低','高'],result
    _assert_png(output,960)
    path.write_text('<qgis><pipe><rasterrenderer type="multibandcolor" redBand="1" greenBand="2" blueBand="3"/></pipe></qgis>')
    result, output = _preview(tmp_path,path)
    assert result['success'] and result['data']['mode']=='requires_layer',result
    assert '没有可独立显示' in result['data']['warnings'][0]


def test_empty_or_missing_layer_preview_fails_clearly(style_project,tmp_path):
    from qgis.core import QgsVectorLayer, QgsProject
    layer = _layer('Point','样式源')
    result, _ = _preview(tmp_path,_save(layer,tmp_path),layer_id='missing')
    assert not result['success'] and '不存在' in result['info'],result
    empty=QgsVectorLayer('Point?crs=EPSG:4326','空图层','memory')
    QgsProject.instance().addMapLayer(empty)
    result, _ = _preview(tmp_path,_save(layer,tmp_path),layer_id=empty.id())
    assert not result['success'] and '没有要素' in result['info'],result


def test_actual_preview_uses_formal_name_field_fallback(style_project, tmp_path):
    from qgis.core import QgsVectorLayer, QgsFeature, QgsGeometry, QgsProject
    from autocarto.tests.support.resource_handles import resolve_handle
    layer=QgsVectorLayer('LineString?crs=EPSG:4326&field=name:string','河流','memory')
    feature=QgsFeature(layer.fields());feature.setAttributes(['真实河流']);feature.setGeometry(QgsGeometry.fromWkt('LINESTRING(0 0,1 1)'))
    layer.dataProvider().addFeatures([feature]);layer.updateExtents();QgsProject.instance().addMapLayer(layer)
    handle=resolve_handle(Path(__file__).resolve().parents[2]/'autocarto/resources/system/styles/河流线','style')
    result, output=_preview(tmp_path,Path(handle['entry']),layer_id=layer.id(),label_field='NAME')
    assert result['success'],result
    assert result['data']['warnings']==['标注字段 NAME 不存在，使用 name']
    _assert_png(output,1440)
    assert not layer.labelsEnabled()


def test_hidden_scale_and_null_geometry_return_explanations(style_project,tmp_path):
    from qgis.core import QgsProject,QgsVectorLayer,QgsFeature
    layer=_layer('Point','比例尺')
    styled=layer.clone();styled.setScaleBasedVisibility(True);styled.setMinimumScale(100);styled.setMaximumScale(1)
    result,_=_preview(tmp_path,_save(styled,tmp_path),layer_id=layer.id())
    assert not result['success'] and '超出样式可见范围' in result['info'],result
    empty=QgsVectorLayer('Point?crs=EPSG:4326','空几何','memory')
    empty.dataProvider().addFeatures([QgsFeature()]);QgsProject.instance().addMapLayer(empty)
    result,_=_preview(tmp_path,_save(layer,tmp_path),layer_id=empty.id())
    assert not result['success'] and '未能' in result['info'],result


def test_late_valid_geometry_is_not_mistaken_for_empty_layer(style_project,tmp_path):
    from qgis.core import QgsProject,QgsVectorLayer,QgsFeature,QgsGeometry
    layer=QgsVectorLayer('Point?crs=EPSG:4326','后部有几何','memory')
    features=[QgsFeature() for _ in range(300)]
    last=QgsFeature();last.setGeometry(QgsGeometry.fromWkt('POINT(2 3)'));features.append(last)
    layer.dataProvider().addFeatures(features);layer.updateExtents();QgsProject.instance().addMapLayer(layer)
    result,output=_preview(tmp_path,_save(layer,tmp_path),layer_id=layer.id())
    assert result['success'],result
    _assert_png(output,1440)


def test_cancelled_request_does_not_recreate_removed_staging_directory(style_project,tmp_path):
    from autocarto.tools.system._shared.resource_preview_impl import resource_preview_handler
    layer=_layer('Point','用户层')
    stage=tmp_path/'removed-stage'
    path=_save(layer,tmp_path)
    result=resource_preview_handler(dict(resource_path=str(path),resource_type='style',layer_id=layer.id(),output_path=str(stage/'preview.png')))
    assert not result['success'] and '取消' in result['info'],result
    assert not stage.exists()


def test_yaml_execution_binding_omits_optional_layer_id(style_project,tmp_path):
    import yaml
    from autocarto.core.tool_engine import QGISToolEngine
    root=Path(__file__).resolve().parents[2]
    definition=yaml.safe_load((root/'autocarto/tools/system/print-layout/resource_preview.yaml').read_text())
    optional=next(p for p in definition['spec']['parameters'] if p['name']=='layer_id')
    assert not optional.get('required') and optional['default']==''
    engine=QGISToolEngine()
    engine.update_contract(execution_manifest=[{'name':definition['metadata']['name'],'parameters':definition['spec']['parameters'],
        'handler_file':str(root/'autocarto/tools/system/print-layout/handlers/resource_preview.py'),'handler_function':'resource_preview_handler'}])
    layer=_layer('Point','符号源');path=_save(layer,tmp_path)
    result=engine.invoke('resource_preview',{'resource_path':str(path),'resource_type':'style','output_path':str(tmp_path/'binding.png')})
    assert result['success'] and result['data']['mode']=='legend',result
