"""Render honest symbol legends or a cloned layer, without editing a project."""
from pathlib import Path
import xml.etree.ElementTree as ET


_STYLE_WIDTH = 960
_MAX_LEGEND_ITEMS = 48
_GEOMETRIES = {"point": "Point", "line": "LineString", "polygon": "Polygon"}
_GEOMETRY_LABELS = {"point": "点", "line": "线", "polygon": "面", "raster": "栅格"}


def _style_geometry(root, params):
    if root.find("pipe") is not None or root.find("rasterrenderer") is not None:
        return "raster"
    geometry = {"0": "point", "1": "line", "2": "polygon"}.get(root.findtext("layerGeometryType"))
    geometry = geometry or params.get("geometry")
    if geometry not in _GEOMETRIES:
        raise ValueError("样式没有可识别的几何类型，请使用有效的 QML 图层样式")
    return geometry


def _font(size, bold=False):
    from qgis.PyQt.QtGui import QFont
    font = QFont()
    font.setFamilies(["PingFang SC", "Noto Sans CJK SC", "Microsoft YaHei", "sans-serif"])
    font.setPixelSize(size)
    font.setBold(bold)
    return font


def _canvas(height):
    from qgis.PyQt.QtGui import QImage, QPainter, QColor
    image = QImage(_STYLE_WIDTH, height, QImage.Format_ARGB32_Premultiplied)
    image.fill(QColor("#ffffff"))
    painter = QPainter(image)
    painter.setRenderHint(QPainter.Antialiasing)
    painter.setRenderHint(QPainter.TextAntialiasing)
    return image, painter


def _text(painter, text, x, y, width, height, size=24, color="#475569", bold=False):
    from qgis.PyQt.QtCore import QRectF, Qt
    from qgis.PyQt.QtGui import QColor
    painter.setFont(_font(size, bold))
    painter.setPen(QColor(color))
    painter.drawText(QRectF(x, y, width, height), Qt.AlignVCenter | Qt.TextWordWrap, str(text))


def _save(image, output):
    if image.isNull() or not image.save(str(output), "PNG"):
        raise ValueError("无法保存资源预览")


def _load_style(layer, path):
    from qgis.core import QgsMapLayer
    # Limit loading to visual settings: the preview must not run form init code
    # or import unrelated form/field definitions from a QML document.
    categories = QgsMapLayer.Symbology | QgsMapLayer.Labeling | QgsMapLayer.Rendering
    message, ok = layer.loadNamedStyle(str(path), categories=categories)
    if not ok:
        raise ValueError("样式无法加载：" + message)
    if hasattr(layer, "labeling"):
        from autocarto.tools.system._shared.style_labeling import restore_qml_font_families
        restore_qml_font_families(layer)


def _scale_description(item):
    minimum, maximum = item.scaleMinDenom(), item.scaleMaxDenom()
    if minimum <= 0 and maximum <= 0:
        return ""
    return "比例尺范围 " + (f"1:{minimum:,}" if minimum > 0 else "不限") + " — " + (f"1:{maximum:,}" if maximum > 0 else "不限")


def _vector_legend(path, output, params, geometry):
    from qgis.core import QgsVectorLayer, QgsRenderContext, QgsSymbolLayerUtils, QgsScreenProperties
    from qgis.PyQt.QtCore import QSize, QRectF
    from qgis.PyQt.QtGui import QColor
    # No made-up attribute values: use the renderer's own legend items.
    layer = QgsVectorLayer(_GEOMETRIES[geometry] + "?crs=EPSG:4326", "符号图例", "memory")
    _load_style(layer, path)
    renderer = layer.renderer()
    if renderer is None:
        raise ValueError("样式没有可用的符号渲染器")
    items = renderer.legendSymbolItems()
    if not items:
        return _message_preview(output, geometry, "此样式依赖实际数据", "请用图层预览查看实际效果。")
    shown = items[:_MAX_LEGEND_ITEMS]
    single = len(items) == 1 and items[0].symbol() is not None
    context = QgsRenderContext()
    # A point glyph needs a different viewport zoom from a page or a long line.
    # Zoom the render context, never the stored symbol's dimensions.
    dpi = (1728 if single else 720) if geometry == "point" else 288
    context.setScaleFactor(dpi / 25.4)
    context.setRendererScale(25000)
    screen = QgsScreenProperties()
    screen.setPhysicalDpi(dpi)
    required = sorted(renderer.usedAttributes(context))
    dependent = bool(required) or any(item.symbol() is not None and item.symbol().hasDataDefinedProperties() for item in items)
    rows = []
    for index, item in enumerate(shown):
        rows.append({"label": item.label() or ("单一符号" if single else f"规则 {index + 1}"),
                     "level": item.level(), "scale": _scale_description(item), "symbol": item.symbol()})
    height = 520 if single else 160 + len(rows) * 124
    image, painter = _canvas(height)
    try:
        _text(painter, _GEOMETRY_LABELS[geometry] + ("符号" if single else "样式图例"), 40, 22, 700, 40, 26, "#334155", True)
        if single:
            symbol = rows[0]["symbol"]
            pixmap = QgsSymbolLayerUtils.symbolPreviewPixmap(symbol, QSize(760, 280), 32, context, False, None, None, screen)
            painter.drawPixmap(100, 90, pixmap)
            if rows[0]["scale"]:
                _text(painter, rows[0]["scale"], 40, 380, 880, 38, 22)
        else:
            for index, row in enumerate(rows):
                y = 82 + index * 124
                painter.setPen(QColor("#e2e8f0"))
                painter.drawLine(40, y + 119, 920, y + 119)
                if row["symbol"] is not None:
                    pixmap = QgsSymbolLayerUtils.symbolPreviewPixmap(row["symbol"], QSize(250, 102), 16, context, False, None, None, screen)
                    painter.drawPixmap(40, y + 7, pixmap)
                indent = min(max(row["level"], 0), 5) * 18
                _text(painter, row["label"], 325 + indent, y + 8, 580 - indent, 62, 26, "#0f172a", row["symbol"] is None)
                if row["scale"]:
                    _text(painter, row["scale"], 325 + indent, y + 75, 580 - indent, 32, 19)
        footer = "符号放大示意 · 标注与实际效果请用图层预览"
        if dependent:
            footer = "依赖数据字段或表达式 · 请用图层预览核对效果"
        if len(items) > len(shown):
            footer = f"显示前 {len(shown)} 项，共 {len(items)} 项 · 用图层预览查看实际效果"
        _text(painter, footer, 40, height - 66, 880, 42, 23, "#64748b")
    finally:
        painter.end()
    _save(image, output)
    return {"geometry": geometry, "width": _STYLE_WIDTH, "height": height, "mode": "legend",
            "renderer": renderer.type(), "legend_count": len(items), "displayed_count": len(shown),
            "labels": [row["label"] for row in rows], "required_fields": required,
            "data_dependent": dependent, "labels_enabled": layer.labelsEnabled()}


def _message_preview(output, geometry, title, message):
    image, painter = _canvas(520)
    try:
        _text(painter, title, 64, 140, 832, 70, 36, "#334155", True)
        _text(painter, message, 64, 240, 832, 120, 28)
    finally:
        painter.end()
    _save(image, output)
    return {"geometry": geometry, "width": _STYLE_WIDTH, "height": 520,
            "mode": "requires_layer", "warnings": [message]}


def _raster_legend(root, output):
    from qgis.PyQt.QtGui import QColor
    from qgis.PyQt.QtCore import QRectF
    renderer = root.find(".//rasterrenderer")
    entries = []
    if renderer is not None:
        for element in renderer.findall(".//colorrampshader/item") + renderer.findall(".//colorPalette/paletteEntry"):
            color = QColor(element.get("color", ""))
            if not color.isValid():
                continue
            color.setAlpha(max(0, min(255, int(float(element.get("alpha", "255"))))))
            entries.append((element.get("label") or element.get("value", ""), color))
    if not entries:
        return _message_preview(output, "raster", "栅格样式需要数据", "此样式没有可独立显示的颜色图例。请选择栅格图层查看实际效果。")
    shown = entries[:_MAX_LEGEND_ITEMS]
    height = 160 + len(shown) * 90
    image, painter = _canvas(height)
    try:
        _text(painter, "栅格颜色图例", 40, 22, 880, 40, 26, "#334155", True)
        for index, (label, color) in enumerate(shown):
            y = 85 + index * 90
            painter.fillRect(QRectF(40, y, 230, 60), color)
            _text(painter, label, 320, y, 600, 60, 25, "#0f172a")
        footer = "颜色节点示意 · 实际像元效果请用图层预览"
        if len(entries) > len(shown):
            footer = f"显示前 {len(shown)} 项，共 {len(entries)} 项 · 实际效果请用图层预览"
        _text(painter, footer, 40, height - 64, 880, 40, 23, "#64748b")
    finally:
        painter.end()
    _save(image, output)
    return {"geometry": "raster", "mode": "legend", "width": _STYLE_WIDTH, "height": height,
            "legend_count": len(entries), "displayed_count": len(shown), "labels": [item[0] for item in shown]}


def _label_settings(layer):
    labeling = layer.labeling()
    if labeling is None:
        return []
    if hasattr(labeling, "rootRule"):
        settings = []
        def visit(rule):
            if rule.settings() is not None:
                settings.append(rule.settings())
            for child in rule.children():
                visit(child)
        visit(labeling.rootRule())
        return settings
    return [labeling.settings()]


def _preview_layer(path, output, params, geometry):
    from qgis.core import QgsProject, QgsVectorLayer, QgsRasterLayer, QgsMapSettings, QgsMapRendererParallelJob, QgsRenderContext, QgsRectangle, QgsFeatureRequest
    from qgis.PyQt.QtCore import QSize
    from qgis.PyQt.QtGui import QColor
    source = QgsProject.instance().mapLayer(str(params["layer_id"]))
    if source is None or not source.isValid():
        raise ValueError("所选图层不存在或已失效，请重新选择图层")
    if geometry == "raster":
        if not isinstance(source, QgsRasterLayer):
            raise ValueError("此样式适用于栅格，请选择栅格图层")
    elif not isinstance(source, QgsVectorLayer) or source.geometryType() != {"point": 0, "line": 1, "polygon": 2}[geometry]:
        raise ValueError("样式与图层几何类型不一致：请选择" + _GEOMETRY_LABELS[geometry] + "图层")
    if isinstance(source, QgsVectorLayer) and source.featureCount() == 0:
        raise ValueError("所选图层没有要素，无法预览样式的实际效果")
    if isinstance(source, QgsVectorLayer):
        request = (QgsFeatureRequest().setFilterExpression('$geometry IS NOT NULL AND NOT is_empty($geometry)')
                   .setNoAttributes().setLimit(1).setTimeout(5000))
        found_geometry = next(source.getFeatures(request), None) is not None
        if not found_geometry:
            raise ValueError("未能从所选图层读取可绘制几何，请确认数据有效后重试")
    layer = source.clone()
    if layer is None or not layer.isValid():
        raise ValueError("无法创建图层的只读预览副本")
    project = QgsProject()
    project.setCrs(source.crs())
    project.addMapLayer(layer)
    try:
        warnings = []
        _load_style(layer, path)
        if isinstance(layer, QgsVectorLayer):
            layer.removeSelection()
            from autocarto.tools.system._shared.style_labeling import resolve_name_field, _override_label_field
            requested = params.get("label_field")
            labeling = layer.labeling()
            # Expressions belong to the QML, not to a guessed name column.
            expression_label = labeling is not None and not hasattr(labeling, "rootRule") and labeling.settings().isExpression
            if expression_label and requested != "无":
                requested = None
            elif not requested and labeling is not None and not hasattr(labeling, "rootRule"):
                requested = labeling.settings().fieldName
            if requested == "无":
                layer.setLabelsEnabled(False)
                layer.setLabeling(None)
            elif requested:
                actual = resolve_name_field(layer.fields().names(), requested)
                if actual:
                    _override_label_field(layer, actual)
                    if actual != requested:
                        warnings.append("标注字段 " + requested + " 不存在，使用 " + actual)
                else:
                    layer.setLabelsEnabled(False)
                    warnings.append("图层没有可用名称字段，按样式套用规则关闭标注")
            context = QgsRenderContext()
            context.setExpressionContext(source.createExpressionContext())
            required = set(layer.renderer().usedAttributes(context))
            if layer.labelsEnabled():
                for setting in _label_settings(layer):
                    required.update(setting.referencedFields(context))
            missing = sorted(required - set(layer.fields().names()) - {"#!allattributes!#"})
            if missing:
                raise ValueError("所选图层缺少样式所需字段：" + "、".join(missing))
        else:
            used_bands = layer.renderer().usesBands()
            if any(band < 1 or band > layer.bandCount() for band in used_bands):
                raise ValueError("所选栅格缺少样式要求的波段")
        extent = layer.extent()
        if not extent.isFinite():
            raise ValueError("所选图层没有可预览的有效范围")
        if extent.width() <= 0 or extent.height() <= 0:
            padding = max(extent.width(), extent.height()) * 0.1 or (0.01 if layer.crs().isGeographic() else 100)
            extent = QgsRectangle(extent.xMinimum() - padding, extent.yMinimum() - padding, extent.xMaximum() + padding, extent.yMaximum() + padding)
        extent.scale(1.12)
        settings = QgsMapSettings()
        settings.setLayers([layer])
        settings.setDestinationCrs(layer.crs())
        settings.setTransformContext(QgsProject.instance().transformContext())
        settings.setExpressionContext(source.createExpressionContext())
        settings.setExtent(extent)
        settings.setOutputSize(QSize(1440, 960))
        settings.setOutputDpi(144)
        settings.setBackgroundColor(QColor("#ffffff"))
        if not layer.isInScaleRange(settings.scale()):
            limits = [f"1:{value:,.0f}" for value in (layer.maximumScale(), layer.minimumScale()) if value > 0]
            raise ValueError(f"当前预览比例尺 1:{settings.scale():,.0f} 超出样式可见范围（{' — '.join(limits)}），请调整样式比例尺范围后重试")
        job = QgsMapRendererParallelJob(settings)
        job.start()
        job.waitForFinished()
        errors = job.errors()
        if errors:
            raise ValueError("样式预览渲染失败：" + "；".join(error.message for error in errors))
        image = job.renderedImage()
        if warnings:
            from qgis.PyQt.QtGui import QImage, QPainter
            framed = QImage(image.width(), image.height() + 92, QImage.Format_ARGB32_Premultiplied)
            framed.fill(QColor("#ffffff"))
            painter = QPainter(framed)
            try:
                painter.drawImage(0, 0, image)
                _text(painter, "；".join(warnings), 32, image.height() + 12, image.width() - 64, 68, 25)
            finally:
                painter.end()
            image = framed
        _save(image, output)
        return {"geometry": geometry, "mode": "layer", "width": image.width(), "height": image.height(),
                "layer_id": source.id(), "layer_name": source.name(), "scale": settings.scale(), "warnings": warnings}
    finally:
        project.clear()


def _style_preview(path, output, params):
    root = ET.parse(path).getroot()
    geometry = _style_geometry(root, params)
    if params.get("layer_id"):
        return _preview_layer(path, output, params, geometry)
    if geometry == "raster":
        return _raster_legend(root, output)
    return _vector_legend(path, output, params, geometry)


def _china_sample(project):
    from qgis.core import QgsVectorLayer,QgsFeature,QgsFillSymbol,QgsSingleSymbolRenderer
    from autocarto.tools.system._shared.tool_assets import tool_asset_path
    source=QgsVectorLayer(str(tool_asset_path("admin-boundaries","中国行政区边界","data","china_province.geojson")),"中国省界","ogr")
    if not source.isValid():raise ValueError("内置省界无法读取，不能生成版面预览")
    layer=QgsVectorLayer("MultiPolygon?crs=EPSG:4326&field=NAME:string","示例省界","memory")
    for f in source.getFeatures():
        geometry=f.geometry().simplify(0.15);geometry.convertToMultiType();out=QgsFeature(layer.fields());out.setGeometry(geometry);out.setAttributes(["示例省份"]);layer.dataProvider().addFeatures([out])
    layer.updateExtents();layer.setRenderer(QgsSingleSymbolRenderer(QgsFillSymbol.createSimple({"color":"#d7e9e1","outline_color":"#718b80","outline_width":"0.18"})));project.setCrs(layer.crs());project.addMapLayer(layer);return layer


def _layout_preview(path,output,params):
    from qgis.core import QgsProject,QgsPrintLayout,QgsLayoutExporter,QgsLayoutItemMap,QgsLayoutItemLabel,QgsReadWriteContext,QgsPathResolver
    from qgis.PyQt.QtCore import QSize
    from qgis.PyQt.QtXml import QDomDocument
    project=QgsProject();layer=_china_sample(project)
    if path.suffix.lower()==".qpt":
        document=QDomDocument();parsed=document.setContent(path.read_text(encoding="utf-8-sig"));ok=parsed[0] if isinstance(parsed,tuple) else parsed
        if not ok:raise ValueError("QPT 无法解析")
        layout=QgsPrintLayout(project);layout.initializeDefaults();ctx=QgsReadWriteContext();ctx.setPathResolver(QgsPathResolver(str(path)));layout.loadFromTemplate(document,ctx)
        for item in layout.items():
            if isinstance(item,QgsLayoutItemMap):item.setLayers([layer]);item.setKeepLayerSet(True);item.setCrs(layer.crs());item.zoomToExtent(layer.extent())
            elif isinstance(item,QgsLayoutItemLabel):
                text=item.text()
                for token in ("{{标题}}","{{title}}","${标题}"):text=text.replace(token,params.get("name") or "版面示例")
                for token in ("{{综述}}","{{overview}}","${综述}"):text=text.replace(token,"示例说明文字，实际内容由制图任务提供。")
                item.setText(text)
    else:
        from autocarto.tools.system._shared.layout_compose_impl import _compose
        handle={"api_version":"resource-handle/v1","type":"layout","id":"layout.preview","name":params.get("name") or "版面预览","version":"1.0.0","entry":str(path),"format":"layout_yaml","entries":[],"total_entries":0}
        result=_compose({"_project":project,"spec":handle,"layout":"预览","title":params.get("name") or "版面示例","overview_text":"示例说明文字。使用真实省界展示版面位置，正式制图时替换为任务数据与来源说明。","map_layers":[layer.id()],"legend_layers":[layer.id()]})
        if not result.get("success"):raise ValueError(result.get("info","版面预览失败"))
        layout=project.layoutManager().layoutByName("预览")
    page=layout.pageCollection().page(0).pageSize();width=1600;height=max(1,round(width*page.height()/page.width()));settings=QgsLayoutExporter.ImageExportSettings();settings.imageSize=QSize(width,height);settings.dpi=width*25.4/page.width()
    result=QgsLayoutExporter(layout).exportToImage(str(output),settings)
    if result!=QgsLayoutExporter.Success:raise ValueError("版面预览导出失败")
    project.clear();return {"width":width,"height":height}


def resource_preview_handler(params):
    try:
        from autocarto.tools.system._shared.layout_runtime import run_in_main_thread
        def render():
            path=Path(params.get("resource_path","")).expanduser();output=Path(params.get("output_path",""))
            if not path.is_absolute() or not path.is_file() or path.is_symlink():raise ValueError("资源路径必须是本机普通文件")
            if not output.is_absolute() or output.suffix.lower()!=".png":raise ValueError("预览输出必须是 PNG 的绝对路径")
            if params.get("resource_type") not in ("style","layout"):raise ValueError("只能预览样式或版面")
            if params.get("layer_id"):
                if not output.parent.is_dir():
                    raise ValueError("预览请求已结束或取消，请重新预览")
            else:
                output.parent.mkdir(parents=True,exist_ok=True)
            if params.get("layer_id") and params["resource_type"] != "style":
                raise ValueError("用图层预览仅适用于样式")
            info=_style_preview(path,output,params) if params["resource_type"]=="style" else _layout_preview(path,output,params)
            return {"success":True,"output":str(output),"info":"资源预览已生成","data":info}
        return run_in_main_thread(render)
    except Exception as exc:
        return {"success":False,"output":"","info":str(exc),"error_type":"resource_preview_failed"}
