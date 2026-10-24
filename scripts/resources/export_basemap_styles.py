"""冻结旧符号构造，用于导出 QML 与旧/新回归；运行时不导入本模块。"""
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))

from typing import (
    Any,
    Dict,
    List,
    Optional,
    Tuple,
)
from qgis.PyQt.QtCore import Qt
from qgis.PyQt.QtGui import (
    QColor,
    QFont,
)
from qgis.core import (
    Qgis,
    QgsFillSymbol,
    QgsUnitTypes,
    QgsLineSymbol,
    QgsMarkerSymbol,
    QgsPalLayerSettings,
    QgsProject,
    QgsProperty,
    QgsSimpleFillSymbolLayer,
    QgsSimpleLineSymbolLayer,
    QgsSingleSymbolRenderer,
    QgsTextBufferSettings,
    QgsVectorLayerSimpleLabeling,
    QgsWkbTypes,
    QgsSimpleMarkerSymbolLayer,
)
from autocarto.tools.system._shared.style_common import (
    _get_layer,
    _wrap_main_thread,
)
from autocarto.tools.system._shared.tool_assets import tool_asset_path


_RIVER_LINE_COLOR = "#a6cee3"


def set_marker_symbol_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:
        layer, error = _get_layer(params.get("layer"))
        if error:
            return error
        if layer.geometryType() != QgsWkbTypes.PointGeometry:
            return {
                "success": False,
                "output": "",
                "info": "错误：仅点图层支持 marker symbol",
            }

        layer_type = str(params.get("layer_type") or "").strip().lower()
        if layer_type not in {"city", "county"}:
            return {
                "success": False,
                "output": "",
                "info": "错误：layer_type 必须是 city 或 county",
            }

        label_field = params.get("label_field")
        if not label_field:
            return {"success": False, "output": "", "info": "错误：缺少标注字段参数 label_field"}
        label_field = str(label_field)
        if layer.fields().indexFromName(label_field) < 0:
            return {
                "success": False,
                "output": "",
                "info": f"错误：图层字段 '{label_field}' 不存在",
            }

        transparent = QColor(0, 0, 0, 0)

        def _circle_layer(fill_color, stroke_color, size_mm: float, stroke_width_mm: float):
            symbol_layer = QgsSimpleMarkerSymbolLayer.create(
                {
                    "name": "circle",
                    "size": str(size_mm),
                    "color": fill_color.name(QColor.HexArgb),
                    "outline_color": stroke_color.name(QColor.HexArgb),
                    "outline_width": str(stroke_width_mm),
                }
            )
            if symbol_layer is None:
                symbol_layer = QgsMarkerSymbol.createSimple(
                    {
                        "name": "circle",
                        "size": str(size_mm),
                        "color": fill_color.name(QColor.HexArgb),
                        "outline_color": stroke_color.name(QColor.HexArgb),
                        "outline_width": str(stroke_width_mm),
                    }
                ).symbolLayer(0).clone()

            if hasattr(symbol_layer, "setSizeUnit"):
                symbol_layer.setSizeUnit(QgsUnitTypes.RenderMillimeters)
            if hasattr(symbol_layer, "setStrokeWidthUnit"):
                symbol_layer.setStrokeWidthUnit(QgsUnitTypes.RenderMillimeters)
            if hasattr(symbol_layer, "setFillColor"):
                symbol_layer.setFillColor(fill_color)
            elif hasattr(symbol_layer, "setColor"):
                symbol_layer.setColor(fill_color)
            if hasattr(symbol_layer, "setStrokeColor"):
                symbol_layer.setStrokeColor(stroke_color)
            if hasattr(symbol_layer, "setStrokeWidth"):
                symbol_layer.setStrokeWidth(stroke_width_mm)
            if stroke_color.alpha() == 0 and hasattr(symbol_layer, "setStrokeStyle"):
                symbol_layer.setStrokeStyle(Qt.NoPen)
            return symbol_layer

        def _marker_symbol(symbol_layers):
            symbol = QgsMarkerSymbol()
            symbol.changeSymbolLayer(0, symbol_layers[0])
            for symbol_layer in symbol_layers[1:]:
                symbol.appendSymbolLayer(symbol_layer)
            return symbol

        city_symbol = _marker_symbol(
            [
                _circle_layer(transparent, QColor("#000000"), 2.2, 0.1),
                _circle_layer(QColor("#000000"), transparent, 0.8, 0.0),
            ]
        )
        county_symbol = _marker_symbol(
            [
                _circle_layer(transparent, QColor("#000000"), 1.8, 0.1),
                _circle_layer(transparent, QColor("#000000"), 0.8, 0.1),
            ]
        )

        symbol = city_symbol if layer_type == "city" else county_symbol
        layer.setRenderer(QgsSingleSymbolRenderer(symbol))

        label_settings = QgsPalLayerSettings()

        if layer.labeling() and hasattr(layer.labeling(), "settings"):
            label_settings = layer.labeling().settings()
        label_settings.fieldName = label_field

        text_format = label_settings.format()
        text_format.setSize(4 if layer_type == "city" else 3)
        text_format.setSizeUnit(QgsUnitTypes.RenderMillimeters)
        label_settings.setFormat(text_format)
        label_properties = label_settings.dataDefinedProperties()
        if hasattr(label_properties, "removeProperty"):
            label_properties.removeProperty(QgsPalLayerSettings.Property.Size)
        else:
            label_properties.setProperty(
                QgsPalLayerSettings.Property.Size,
                QgsProperty(),
            )

        layer.setLabeling(QgsVectorLayerSimpleLabeling(label_settings))
        layer.setLabelsEnabled(True)

        layer.triggerRepaint()
        return {
            "success": True,
            "output": layer.name(),
            "info": "点图层符号已更新",
        }
    except Exception as e:
        return {"success": False, "output": "", "info": f"设置点图层符号失败: {str(e)}"}



def _set_line_layer_units(line_layer):
    if hasattr(line_layer, "setWidthUnit"):
        line_layer.setWidthUnit(QgsUnitTypes.RenderMillimeters)
    if hasattr(line_layer, "setOffsetUnit"):
        line_layer.setOffsetUnit(QgsUnitTypes.RenderMillimeters)



def _gb_line_layer(
    color: str,
    width_mm: float,
    pen_style=Qt.SolidLine,
    offset_mm: float = 0.0,
    dash_pattern: Optional[List[float]] = None,
    cap_style=Qt.FlatCap,
    join_style=Qt.MiterJoin,
):
    line_layer = QgsSimpleLineSymbolLayer()
    line_layer.setColor(QColor(color))
    line_layer.setWidth(float(width_mm))
    line_layer.setPenStyle(pen_style)
    if offset_mm and hasattr(line_layer, "setOffset"):
        line_layer.setOffset(float(offset_mm))
    if dash_pattern and hasattr(line_layer, "setUseCustomDashPattern"):
        line_layer.setUseCustomDashPattern(True)
        line_layer.setCustomDashVector([float(item) for item in dash_pattern])
    if hasattr(line_layer, "setPenCapStyle"):
        line_layer.setPenCapStyle(cap_style)
    if hasattr(line_layer, "setPenJoinStyle"):
        line_layer.setPenJoinStyle(join_style)
    _set_line_layer_units(line_layer)
    return line_layer



def _gb_line_symbol(symbol_layers: List[QgsSimpleLineSymbolLayer]):
    symbol = QgsLineSymbol()
    symbol.changeSymbolLayer(0, symbol_layers[0])
    for symbol_layer in symbol_layers[1:]:
        symbol.appendSymbolLayer(symbol_layer)
    return symbol



def _line_symbol_by_type(
    line_type: str,
) -> Tuple[Optional[QgsLineSymbol], Optional[str]]:
    normalized_type = str(line_type or "").strip()
    if normalized_type == "铁路":
        return (
            _gb_line_symbol(
                [
                    _gb_line_layer("#000000", 0.4),
                    _gb_line_layer("#ffffff", 0.4, dash_pattern=[4.00, 4.00]),
                ]
            ),
            "铁路",
        )
    if normalized_type == "高速公路":
        return (
            _gb_line_symbol(
                [
                    _gb_line_layer("#000000", 0.4),
                    _gb_line_layer("#c0c0c0", 0.3),
                ]
            ),
            "高速公路",
        )
    if normalized_type == "国道":
        return (
            _gb_line_symbol(
                [
                    _gb_line_layer("#ffb957", 0.30),
                ]
            ),
            "国道",
        )
    if normalized_type == "省界":
        return (
            _gb_line_symbol(
                [
                    _gb_line_layer("#2399e9", 0.5),
                    _gb_line_layer("#a6cee3", 0.5, offset_mm = 0.4),
                ]
            ),
            "省界",
        )
    if normalized_type == "河流":
        return (
            _gb_line_symbol(
                [
                    _gb_line_layer(_RIVER_LINE_COLOR, 0.46),
                ]
            ),
            "河流",
        )
    return None, None



def set_line_symbol_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:
        layer_name = params.get("layer")
        layer, error = _get_layer(layer_name)
        if error:
            return error
        if layer.geometryType() != QgsWkbTypes.LineGeometry:
            return {
                "success": False,
                "output": "",
                "info": "错误：仅线图层支持 line symbol",
            }
        line_type = str(params.get("line_type") or "").strip()
        symbol, style_name = _line_symbol_by_type(line_type)
        if symbol is None:
            return {
                "success": False,
                "output": "",
                "info": "错误：line_type 必须是“铁路”“高速公路”“国道”“省界”或“河流”",
            }

        label_field = str(params.get("label_field") or "").strip()
        # 河流名称字段是底图的固定约定。即使调用方漏传 label_field，
        # 也不要让河流图层继续沿用上一次运行留下的标注状态。
        if line_type == "河流" and not label_field:
            if layer.fields().indexFromName("NAME") >= 0:
                label_field = "NAME"
        if label_field and layer.fields().indexFromName(label_field) < 0:
            return {
                "success": False,
                "output": "",
                "info": f"错误：图层字段 '{label_field}' 不存在",
            }

        layer.setRenderer(QgsSingleSymbolRenderer(symbol))

        label_updated = False
        if line_type == "河流" and label_field:
            # 河流线是线图层，必须使用 QGIS 对线图层的默认标注配置。
            # 裸 QgsPalLayerSettings() 的 placement 默认是 AroundPoint（点/面模式），
            # 用在线图层上会导致一个标签都不显示。这里直接取线图层默认设置，
            # 与用户在属性里手动打开标注的行为一致（placement = Line）。
            label_settings = QgsVectorLayerSimpleLabeling.defaultSettingsForLayer(layer)
            label_settings.fieldName = label_field
            # 河流线被拆成很多端点相接的小段，默认每段都会标一个，导致河名密密麻麻。
            # 合并相连线，让一条连续河段只标一个名字。
            label_settings.mergeLines = True
            river_text_format = label_settings.format()
            river_text_format.setColor(QColor(_RIVER_LINE_COLOR))
            label_settings.setFormat(river_text_format)
            layer.setLabeling(QgsVectorLayerSimpleLabeling(label_settings))
            layer.setLabelsEnabled(True)
            # 回读一次，避免工具返回“成功”但标注实际没有启用。
            applied_settings = layer.labeling().settings()
            if not layer.labelsEnabled() or applied_settings.fieldName != label_field:
                return {
                    "success": False,
                    "output": "",
                    "info": f"河流名称标注未成功写入图层：{layer.name()}（字段 {label_field}）",
                }
            QgsProject.instance().setDirty(True)
            label_updated = True

        layer.triggerRepaint()
        return {
            "success": True,
            "output": layer.name(),
            "info": (
                f"线图层符号和河流名称标注已更新：{style_name}（{label_field}）"
                if label_updated
                else f"线图层符号已更新：{style_name}"
            ),
        }
    except Exception as e:
        return {"success": False, "output": "", "info": f"设置线图层符号失败: {str(e)}"}



def set_fill_symbol_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:
        layer, error = _get_layer(params.get("layer"))
        if error:
            return error
        if layer.geometryType() != QgsWkbTypes.PolygonGeometry:
            return {
                "success": False,
                "output": "",
                "info": "错误：仅面图层支持 fill symbol",
            }

        layer_type_aliases = {
            "县": "county",
            "county": "county",
            "市": "city",
            "city": "city",
            "省": "province",
            "province": "province",
            "河流": "river",
            "river": "river",
        }
        layer_type_raw = str(params.get("layer_type") or "").strip()
        layer_type = layer_type_aliases.get(layer_type_raw.lower()) or layer_type_aliases.get(layer_type_raw)
        if layer_type not in {"county", "city", "province", "river"}:
            return {
                "success": False,
                "output": "",
                "info": "错误：layer_type 必须是 县、市、省 或 河流",
            }

        label_field = str(params.get("label_field") or "").strip()
        if label_field and layer.fields().indexFromName(label_field) < 0:
            return {
                "success": False,
                "output": "",
                "info": f"错误：图层字段 '{label_field}' 不存在",
            }

        fill_layer = QgsSimpleFillSymbolLayer()
        fill_layer.setBrushStyle(Qt.SolidPattern)
        if layer_type == "river":
            fill_layer.setFillColor(QColor("#a6cee3"))
            fill_layer.setStrokeColor(QColor("#a6cee3"))
            fill_layer.setStrokeWidth(0.26)
            fill_layer.setStrokeStyle(Qt.SolidLine)
        else:
            fill_layer.setFillColor(QColor("#ffffff"))
            fill_layer.setStrokeColor(QColor("#707070"))
            fill_layer.setStrokeWidth(0.5 if layer_type == "province" else 0.2)
            fill_layer.setStrokeStyle(Qt.DashDotLine)

        if hasattr(fill_layer, "setStrokeWidthUnit"):
            fill_layer.setStrokeWidthUnit(QgsUnitTypes.RenderMillimeters)

        symbol = QgsFillSymbol()
        symbol.changeSymbolLayer(0, fill_layer)
        layer.setRenderer(QgsSingleSymbolRenderer(symbol))

        label_updated = False
        if label_field:
            label_size_mm = {
                "county": 2.2,
                "city": 3.0,
                "province": 3.8,
                "river": 2.6,
            }[layer_type]
            label_settings = QgsPalLayerSettings()
            if layer.labeling() and hasattr(layer.labeling(), "settings"):
                label_settings = layer.labeling().settings()
            label_settings.fieldName = label_field
            try:
                label_settings.placement = Qgis.LabelPlacement.Horizontal
            except AttributeError:
                label_settings.placement = QgsPalLayerSettings.Horizontal

            text_format = label_settings.format()
            label_font = QFont()
            # 地图标注用“微软雅黑”本体，而不是 UI 专用字体；mac 上没有微软雅黑时
            # 回退到思源黑体/冬青黑体，避免悄悄退成系统默认字体。
            label_font.setFamilies(["Microsoft YaHei", "Noto Sans CJK SC", "Hiragino Sans GB"])
            label_font.setBold(True)
            text_format.setFont(label_font)
            text_format.setSize(label_size_mm)
            text_format.setSizeUnit(QgsUnitTypes.RenderMillimeters)
            text_format.setColor(QColor("#242424"))
            if layer_type == "province":
                # 省名是跨省底图的主层级标注，不能被县级地名或路线标注
                # 直接压掉；但不把省名设为其他标注的障碍，避免它们
                # 大面积消失。
                if hasattr(label_settings, "displayAll"):
                    label_settings.displayAll = True
                if hasattr(label_settings, "obstacle"):
                    label_settings.obstacle = False
                if hasattr(label_settings, "priority"):
                    label_settings.priority = 5
                if hasattr(label_settings, "allowDegradedPlacement"):
                    label_settings.allowDegradedPlacement = True
                text_format.setSize(5.2)
                province_buffer = QgsTextBufferSettings()
                province_buffer.setEnabled(True)
                province_buffer.setSize(0.7)
                province_buffer.setSizeUnit(QgsUnitTypes.RenderMillimeters)
                province_buffer.setColor(QColor("#ffffff"))
                text_format.setBuffer(province_buffer)
            label_settings.setFormat(text_format)
            label_properties = label_settings.dataDefinedProperties()
            if hasattr(label_properties, "removeProperty"):
                label_properties.removeProperty(QgsPalLayerSettings.Property.Size)
            else:
                label_properties.setProperty(
                    QgsPalLayerSettings.Property.Size,
                    QgsProperty(),
                )

            layer.setLabeling(QgsVectorLayerSimpleLabeling(label_settings))
            layer.setLabelsEnabled(True)
            applied_settings = layer.labeling().settings()
            if not layer.labelsEnabled() or applied_settings.fieldName != label_field:
                return {
                    "success": False,
                    "output": "",
                    "info": f"面图层标注未成功写入图层：{layer.name()}（字段 {label_field}）",
                }
            QgsProject.instance().setDirty(True)
            label_updated = True
        layer.triggerRepaint()
        return {
            "success": True,
            "output": layer.name(),
            "info": (
                f"面图层填充符号和标注已更新：{layer_type_raw}"
                if label_updated
                else f"面图层填充符号已更新：{layer_type_raw}"
            ),
        }
    except Exception as e:
        return {"success": False, "output": "", "info": f"设置面图层填充失败: {str(e)}"}


set_marker_symbol_handler = _wrap_main_thread(set_marker_symbol_handler)
set_line_symbol_handler = _wrap_main_thread(set_line_symbol_handler)
set_fill_symbol_handler = _wrap_main_thread(set_fill_symbol_handler)



def export_styles(output_dir):
    import csv
    import json
    from qgis.core import QgsMapLayer, QgsVectorLayer, QgsVectorLayerSimpleLabeling

    output_dir = Path(output_dir)
    config = json.loads(tool_asset_path("cartography", "basemap-legacy-presets.json").read_text(encoding="utf-8"))
    dispatch = {"marker": set_marker_symbol_handler, "line": set_line_symbol_handler, "fill": set_fill_symbol_handler}
    for index, entry in enumerate(config["entries"]):
        layer = QgsVectorLayer(entry["geometry"] + "?crs=EPSG:4326&field=name:string&field=NAME:string", "preset_" + str(index), "memory")
        if not layer.isValid():
            raise RuntimeError("无法创建预设源图层")
        QgsProject.instance().addMapLayer(layer)
        try:
            result = dispatch[entry["legacy_handler"]](dict(entry["params"], layer=layer.name()))
            if not result.get("success"):
                raise RuntimeError(str(result))
            if layer.labeling() is not None:
                settings = layer.labeling().settings()
                text_format = settings.format()
                requested = text_format.font().families()
                # QGIS 不会把 QFont 内的回退列表自动写入 QML families。
                if requested != [text_format.font().family()]:
                    text_format.setFamilies(requested)
                    settings.setFormat(text_format)
                    layer.setLabeling(QgsVectorLayerSimpleLabeling(settings))
            message, ok = layer.saveNamedStyle(str(output_dir / entry["style_file"]), categories=QgsMapLayer.Symbology | QgsMapLayer.Labeling)
            if not ok:
                raise RuntimeError(message)
        finally:
            QgsProject.instance().removeMapLayer(layer.id())
    with (output_dir / "对照表.csv").open("w", encoding="utf-8-sig", newline="") as stream:
        writer = csv.writer(stream, lineterminator="\n")
        writer.writerow(["图层", "别名", "样式文件", "标注字段", "说明"])
        for entry in config["entries"]:
            writer.writerow([entry["layer"], "、".join(entry["aliases"]), entry["style_file"], entry["label_field"], entry["description"]])
    return len(config["entries"])


def main():
    import argparse
    import json
    import os
    import tempfile
    from qgis.core import QgsApplication

    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--out", required=True, help="导出 QML 与样式对照表的目标目录")
    args = parser.parse_args()
    output = Path(args.out).expanduser().resolve()
    if QgsApplication.instance() is not None:
        raise RuntimeError("导出脚本必须在独立 QGIS 进程运行")
    output.mkdir(parents=True, exist_ok=True)
    os.environ["QT_QPA_PLATFORM"] = "offscreen"
    contents = Path(sys.executable).resolve().parents[1]
    if sys.platform == "darwin" and (contents / "Resources/qgis").is_dir():
        QgsApplication.setPrefixPath(str(contents / "MacOS"), True)
        QgsApplication.setPkgDataPath(str(contents / "Resources/qgis"))
        QgsApplication.setPluginPath(str(contents / "PlugIns/qgis"))
    with tempfile.TemporaryDirectory(prefix=".style-export-", dir=output) as profile:
        application = QgsApplication([], False, profile)
        application.initQgis()
        try:
            count = export_styles(output)
            print(json.dumps({"success": True, "style_count": count, "output": str(output)}, ensure_ascii=False))
        finally:
            QgsProject.instance().clear()
            application.exitQgis()


if __name__ == "__main__":
    main()
