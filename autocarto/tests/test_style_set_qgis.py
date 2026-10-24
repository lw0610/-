"""真实旧构造、QML 兼容包装和新样式集的符号/标注/像素回归。"""

import csv
import os
from pathlib import Path
import re
from unittest import mock
import xml.etree.ElementTree as ET

import pytest

from autocarto.tests.golden.qgis_bootstrap import isolated_qgis, qgis_available
from autocarto.tools.system._shared.style_set_support import preset_catalog
from autocarto.tests.support.resource_handles import load_handle, resolve_handle


def builtin_style_dir():
    return Path(load_handle("gb-basemap")["root"])


PRESETS = preset_catalog()["entries"]


@pytest.fixture(scope="module")
def style_qgis_runtime(tmp_path_factory):
    if not qgis_available():
        pytest.skip("需要可导入 QGIS 的 Python 环境")
    from qgis.core import QgsApplication
    if QgsApplication.instance() is not None:
        pytest.skip("样式回归必须在独立 QGIS 进程运行")
    with mock.patch.dict(os.environ, {}, clear=False):
        with isolated_qgis(tmp_path_factory.mktemp("style-runtime")):
            yield


@pytest.fixture
def style_project(style_qgis_runtime, tmp_path, monkeypatch):
    from qgis.core import QgsProject
    monkeypatch.setenv("AUTOCARTO_PROJECT_ROOT", str(tmp_path))
    monkeypatch.setenv("AUTOCARTO_HOME", str(tmp_path / "runtime"))
    project = QgsProject.instance()
    project.clear()
    yield project, tmp_path
    project.clear()


def _layer(geometry, name):
    from qgis.core import QgsFeature, QgsGeometry, QgsProject, QgsVectorLayer
    layer = QgsVectorLayer(geometry + "?crs=EPSG:4326&field=name:string&field=NAME:string&field=custom:string", name, "memory")
    wkt = {"Point": "POINT(2 1.5)", "LineString": "LINESTRING(0 0,2 1.5,4 3)",
           "Polygon": "POLYGON((0 0,4 0,4 3,0 3,0 0))"}[geometry]
    feature = QgsFeature(layer.fields())
    feature.setGeometry(QgsGeometry.fromWkt(wkt))
    feature.setAttributes(["示例名称", "示例名称", "自定义名称"])
    assert layer.dataProvider().addFeatures([feature])[0]
    layer.updateExtents()
    QgsProject.instance().addMapLayer(layer)
    return layer


def test_style_set_xlsx_mapping_with_instruction_sheet_is_applied(style_project, tmp_path):
    import shutil
    from autocarto.tests.test_style_set import write_xlsx
    from autocarto.tools.system._shared.style_set_impl import apply_style_set_handler
    entry = next(value for value in PRESETS if value["geometry"] == "Polygon")
    shutil.copyfile(builtin_style_dir() / entry["style_file"], tmp_path / "selected.qml")
    write_xlsx(tmp_path / "对照表.xlsx", [[["说明页"]],
        [["图层", "样式文件", "标注字段"], ["xlsx-area", "selected.qml", "custom"]]])
    layer = _layer("Polygon", "xlsx-area")
    result = apply_style_set_handler({"style_set": resolve_handle(tmp_path, "style_set"), "layers": [layer.id()], "strict": True})
    assert result["success"], result
    assert result["data"]["applied"][0]["style_file"] == str(tmp_path / "selected.qml")
    assert layer.labelsEnabled()
    assert layer.labeling().settings().fieldName == "custom"


def test_legacy_preset_does_not_resolve_the_public_style_directory(style_project, monkeypatch):
    from autocarto.tools.system._shared import basemap_presets_impl, style_set_support
    assert not hasattr(style_set_support, "builtin_style_dir")
    monkeypatch.setattr(style_set_support, "matching_style", lambda *args: pytest.fail("兼容工具不应旁路解析公共资源"))
    prototype = basemap_presets_impl._preset_layer(PRESETS[0])
    assert prototype.isValid() and prototype.renderer() is not None


def _dispatch(module, entry, layer, params=None):
    handler = getattr(module, "set_" + entry["legacy_handler"] + "_symbol_handler")
    result = handler(dict(entry["params"] if params is None else params, layer=layer.name()))
    assert result["success"], result
    return result


def _canonical_xml(element):
    from qgis.PyQt.QtXml import QDomDocument
    document = QDomDocument()
    document.appendChild(document.importNode(element, True))
    root = ET.fromstring(document.toString())
    def canonical(node):
        attributes = []
        for key, value in node.attrib.items():
            if key == "id" and re.fullmatch(r"\{[0-9a-fA-F-]+\}", value):
                value = "<symbol-id>"
            elif value.startswith("<"):
                try:
                    nested = ET.fromstring(value)
                except ET.ParseError:
                    pass
                else:
                    value = canonical(nested)
            attributes.append((key, value))
        # 标准 families 是回退列表的序列化载体；实际 QFont 列表在 _state 单独严格比较。
        return node.tag, tuple(sorted(attributes)), (node.text or "").strip(), tuple(canonical(child) for child in node if child.tag != "families")
    return canonical(root)


def _state(layer):
    from qgis.PyQt.QtXml import QDomDocument
    from qgis.core import QgsReadWriteContext
    document = QDomDocument()
    renderer = _canonical_xml(layer.renderer().save(document, QgsReadWriteContext()))
    labeling = layer.labeling()
    labels = _canonical_xml(labeling.save(document, QgsReadWriteContext())) if labeling is not None else None
    font = labeling.settings().format().font() if labeling is not None else None
    return {"renderer_dump": layer.renderer().dump(), "renderer": renderer,
            "labels_enabled": layer.labelsEnabled(), "labeling": labels, "opacity": layer.opacity(),
            "font_families": (font.families() or [font.family()]) if font is not None else None}


def _render(layer):
    from qgis.PyQt.QtCore import QSize
    from qgis.PyQt.QtGui import QColor
    from qgis.core import QgsMapRendererParallelJob, QgsMapSettings, QgsRectangle
    settings = QgsMapSettings()
    settings.setLayers([layer])
    settings.setDestinationCrs(layer.crs())
    settings.setExtent(QgsRectangle(-1, -1, 5, 4))
    settings.setOutputSize(QSize(400, 300))
    settings.setOutputDpi(96)
    settings.setBackgroundColor(QColor("white"))
    job = QgsMapRendererParallelJob(settings)
    job.start()
    job.waitForFinished()
    image = job.renderedImage()
    assert not image.isNull()
    assert any(value != 255 for value in image.constBits().asstring(image.sizeInBytes()))
    return image


@pytest.mark.parametrize("entry", PRESETS, ids=[entry["style_file"] for entry in PRESETS])
def test_style_legacy_wrappers_match_frozen_old_constructor_and_pixels(style_project, entry):
    from autocarto.tools.system._shared import basemap_presets_impl as current
    from scripts.resources import export_basemap_styles as legacy
    reference = _layer(entry["geometry"], "reference")
    wrapped = _layer(entry["geometry"], "wrapped")
    old_result = _dispatch(legacy, entry, reference)
    new_result = _dispatch(current, entry, wrapped)
    assert old_result["info"] == new_result["info"]
    assert _state(reference) == _state(wrapped)
    assert _render(reference) == _render(wrapped)


@pytest.mark.parametrize("entry", PRESETS, ids=lambda entry: entry["style_file"])
def test_style_legacy_wrappers_preserve_existing_label_settings_selectively(style_project, entry):
    from qgis.PyQt.QtGui import QColor, QFont
    from qgis.core import QgsPalLayerSettings, QgsProperty, QgsTextBufferSettings, QgsTextFormat, QgsVectorLayerSimpleLabeling
    from autocarto.tools.system._shared import basemap_presets_impl as current
    from scripts.resources import export_basemap_styles as legacy
    reference = _layer(entry["geometry"], "reference")
    wrapped = _layer(entry["geometry"], "wrapped")
    for layer in (reference, wrapped):
        settings = QgsPalLayerSettings()
        settings.fieldName = "custom"
        settings.dist = 7.5
        settings.priority = 3
        format = QgsTextFormat()
        font = QFont("monospace")
        font.setItalic(True)
        format.setFont(font)
        format.setSize(9)
        format.setColor(QColor("#347821"))
        buffer = QgsTextBufferSettings()
        buffer.setEnabled(True)
        buffer.setSize(0.3)
        format.setBuffer(buffer)
        settings.setFormat(format)
        properties = settings.dataDefinedProperties()
        properties.setProperty(QgsPalLayerSettings.Property.Size, QgsProperty.fromExpression("12"))
        settings.setDataDefinedProperties(properties)
        layer.setLabeling(QgsVectorLayerSimpleLabeling(settings))
        layer.setLabelsEnabled(True)
        layer.setOpacity(0.42)
    _dispatch(legacy, entry, reference)
    _dispatch(current, entry, wrapped)
    assert _state(reference) == _state(wrapped)
    if entry["legacy_handler"] != "marker":
        params = dict(entry["params"])
        params.pop("label_field", None)
        _dispatch(legacy, entry, reference, params)
        _dispatch(current, entry, wrapped, params)
        assert _state(reference) == _state(wrapped)


def test_style_legacy_parameter_errors_remain_identical(style_project):
    from autocarto.tools.system._shared import basemap_presets_impl as current
    from scripts.resources import export_basemap_styles as legacy
    point, line, fill = _layer("Point", "point"), _layer("LineString", "line"), _layer("Polygon", "fill")
    cases = [("marker", {}), ("line", {}), ("fill", {}),
             ("marker", {"layer": "fill", "layer_type": "city", "label_field": "name"}),
             ("line", {"layer": "point", "line_type": "铁路"}),
             ("fill", {"layer": "line", "layer_type": "省"}),
             ("marker", {"layer": "point", "layer_type": "bad", "label_field": "name"}),
             ("line", {"layer": "line", "line_type": "bad"}),
             ("fill", {"layer": "fill", "layer_type": "bad"}),
             ("marker", {"layer": "point", "layer_type": "city"}),
             ("marker", {"layer": "point", "layer_type": "city", "label_field": "missing"}),
             ("line", {"layer": "line", "line_type": "河流", "label_field": "missing"}),
             ("fill", {"layer": "fill", "layer_type": "省", "label_field": "missing"})]
    for kind, params in cases:
        assert getattr(legacy, "set_" + kind + "_symbol_handler")(params) == getattr(current, "set_" + kind + "_symbol_handler")(params)


def test_basemap_line_helpers_build_equivalent_symbols(style_project):
    from qgis.core import QgsReadWriteContext, QgsSingleSymbolRenderer
    from qgis.PyQt.QtXml import QDomDocument
    from autocarto.tools.system._shared import basemap_presets_impl as current
    from scripts.resources import export_basemap_styles as legacy
    symbols = []
    for module in (legacy, current):
        layers = [module._gb_line_layer("#123456", 0.3, offset_mm=0.2),
                  module._gb_line_layer("#234567", 0.5, dash_pattern=[2, 1])]
        symbols.append(QgsSingleSymbolRenderer(module._gb_line_symbol(layers)))
    document = QDomDocument()
    assert _canonical_xml(symbols[0].save(document, QgsReadWriteContext())) == _canonical_xml(symbols[1].save(document, QgsReadWriteContext()))


def test_style_set_eleven_resources_match_old_default_labels_and_repeat(style_project):
    from scripts.resources import export_basemap_styles as legacy
    from autocarto.tools.system._shared.style_set_impl import apply_style_set_handler
    expected, actual = {}, {}
    for index, entry in enumerate(PRESETS):
        reference = _layer(entry["geometry"], "reference_" + str(index))
        target = _layer(entry["geometry"], entry["layer"])
        params = dict(entry["params"])
        if entry["label_field"] == "无":
            params.pop("label_field", None)
        _dispatch(legacy, entry, reference, params)
        expected[entry["layer"]] = reference
        actual[entry["layer"]] = target
    first = apply_style_set_handler({"style_set": load_handle("gb-basemap"), "layers": list(actual), "strict": True})
    assert first["success"], first
    assert len(first["data"]["applied"]) == 11
    assert not first["data"]["unmatched_layers"] and not first["data"]["unused_styles"]
    states = {name: _state(layer) for name, layer in actual.items()}
    for name, layer in actual.items():
        assert _state(expected[name]) == states[name], name
        assert _render(expected[name]) == _render(layer), name
    second = apply_style_set_handler({"style_set": load_handle("gb-basemap"), "layers": [layer.id() for layer in actual.values()], "strict": True})
    assert second["success"], second
    assert {name: _state(layer) for name, layer in actual.items()} == states


def _custom_set(root, rows):
    path = root / "对照表.csv"
    with path.open("w", encoding="utf-8-sig", newline="") as stream:
        writer = csv.writer(stream)
        writer.writerow(["图层", "别名", "样式文件", "标注字段"])
        writer.writerows(rows)
    return path


def test_style_set_preflight_missing_fields_strict_unmatched_and_invalid_input_do_not_mutate(style_project):
    from autocarto.tools.system._shared.style_set_impl import apply_style_set_handler
    project, root = style_project
    layer = _layer("Point", "县级城市驻地")
    initial = _state(layer)
    strict = apply_style_set_handler({"style_set": load_handle("gb-basemap"), "layers": [layer.id(), "not_found"], "strict": True})
    assert strict["error_type"] == "style_layers_unmatched"
    assert _state(layer) == initial
    resource = root / "point.qml"
    resource.write_bytes((builtin_style_dir() / "县级城市驻地.qml").read_bytes())
    path = _custom_set(root, [[layer.name(), "", resource.name, "missing_field"]])
    missing = apply_style_set_handler({"style_set": resolve_handle(path.parent, "style_set")})
    assert missing["error_type"] == "label_field_missing"
    assert _state(layer) == initial
    path.write_text("图层,说明\na,没有样式列\n", encoding="utf-8")
    with pytest.raises(ValueError, match="FLOW_RESOURCE_INVALID"):
        resolve_handle(path.parent, "style_set")
    assert apply_style_set_handler({})["success"] is False
    assert _state(layer) == initial


def test_style_set_rolls_back_all_changed_layers_when_qgis_rejects_qml(style_project):
    from autocarto.tools.system._shared.style_set_impl import apply_style_set_handler
    project, root = style_project
    first, second = _layer("Point", "A"), _layer("Point", "B")
    old = {layer.id(): _state(layer) for layer in (first, second)}
    (root / "good.qml").write_bytes((builtin_style_dir() / "县级城市驻地.qml").read_bytes())
    (root / "bad.qml").write_bytes((builtin_style_dir() / "县级城市驻地.qml").read_bytes())
    table = _custom_set(root, [["A", "", "good.qml", "name"], ["B", "", "bad.qml", ""]])
    with mock.patch.object(second, "loadNamedStyle", return_value=("样式文件在预检后不可读取", False)):
        result = apply_style_set_handler({"style_set": resolve_handle(table.parent, "style_set"), "layers": [first.id(), second.id()]})
    assert result["success"] is False, result
    assert {layer.id(): _state(layer) for layer in (first, second)} == old


def test_style_set_unknown_qgis_renderer_is_rejected_before_mutation(style_project):
    from autocarto.tools.system._shared.style_set_impl import apply_style_set_handler
    project, root = style_project
    layer = _layer("Point", "A")
    before = _state(layer)
    (root / "bad.qml").write_text('<qgis styleCategories="Symbology"><renderer-v2 type="not_a_renderer"/><layerGeometryType>0</layerGeometryType></qgis>')
    table = _custom_set(root, [["A", "", "bad.qml", ""]])
    result = apply_style_set_handler({"style_set": resolve_handle(table.parent, "style_set"), "layers": [layer.id()]})
    assert result["error_type"] == "style_renderer_unavailable"
    assert _state(layer) == before


def test_style_set_field_override_preserves_label_rules_and_filters(style_project):
    from qgis.core import QgsMapLayer, QgsPalLayerSettings, QgsRuleBasedLabeling
    from autocarto.tools.system._shared.style_set_impl import apply_style_set_handler
    project, root = style_project
    layer = _layer("Point", "A")
    rule_root = QgsRuleBasedLabeling.Rule(QgsPalLayerSettings())
    for field, expression in [("name", '"name" IS NOT NULL'), ("NAME", '"NAME" IS NOT NULL')]:
        settings = QgsPalLayerSettings()
        settings.fieldName = field
        rule = QgsRuleBasedLabeling.Rule(settings)
        rule.setFilterExpression(expression)
        rule_root.appendChild(rule)
    layer.setLabeling(QgsRuleBasedLabeling(rule_root))
    layer.setLabelsEnabled(True)
    message, ok = layer.saveNamedStyle(str(root / "rules.qml"), categories=QgsMapLayer.StyleCategory.Symbology | QgsMapLayer.StyleCategory.Labeling)
    assert ok, message
    table = _custom_set(root, [["A", "", "rules.qml", "custom"]])
    result = apply_style_set_handler({"style_set": resolve_handle(table.parent, "style_set"), "layers": [layer.id()]})
    assert result["success"], result
    assert layer.labeling().type() == "rule-based"
    rules = layer.labeling().rootRule().children()
    assert [rule.filterExpression() for rule in rules] == ['"name" IS NOT NULL', '"NAME" IS NOT NULL']
    assert [rule.settings().fieldName for rule in rules] == ["custom", "custom"]
    assert layer.labeling().rootRule().settings().fieldName == ""
    assert result["data"]["applied"][0]["label_field"] == "custom"


def test_style_set_authority_binding_and_individual_qml_tools(style_project):
    from autocarto.core.tool_engine.engine import QGISToolEngine, execute_tool
    from autocarto.core.paths import get_plugin_root
    from autocarto.tools.system._shared.style_basic_impl import load_style_handler, save_style_handler
    project, root = style_project
    layers = [_layer("Point", "县级城市驻地"), _layer("Polygon", "省域")]
    engine = QGISToolEngine(load_interactive_tools=False)
    engine.update_contract(execution_manifest=[{"name": "style_set_apply", "runtime": "qgis", "domain": "cartography",
        "handler_file": str(get_plugin_root() / "tools/system/cartography/handlers/style_sets.py"), "handler_function": "apply_style_set_handler"}])
    result = execute_tool("style_set_apply", {"style_set": load_handle("gb-basemap"), "layers": [layer.id() for layer in layers], "strict": True}, engine=engine)
    assert result["success"], result
    assert [item["layer_id"] for item in result["output"]["data"]["applied"]] == [layer.id() for layer in layers]
    for index, layer in enumerate(layers):
        output = root / ("saved_" + str(index) + ".qml")
        assert save_style_handler({"layer": layer.name(), "path": str(output)})["success"]
        expected = _state(layer)
        expected_image = _render(layer)
        assert load_style_handler({"layer": layer.name(), "style": resolve_handle(output, "style")})["success"]
        assert _state(layer) == expected
        assert _render(layer) == expected_image



def test_style_set_consumes_go_label_modes_and_resource_summary(style_project):
    from autocarto.tools.system._shared.style_set_impl import apply_style_set_handler
    handle = load_handle("style-set-modes")
    layers = {entry["name"]: _layer("Polygon", entry["name"]) for entry in handle["entries"]}
    result = apply_style_set_handler({"style_set": handle, "strict": True})
    assert result["success"], result
    assert result["resource"] == {key: handle[key] for key in ("id", "name", "version")}
    assert result["warnings"] == handle["warnings"]
    assert layers["保留标注"].labelsEnabled()
    assert layers["保留标注"].labeling().settings().fieldName == "NAME"
    assert not layers["关闭标注"].labelsEnabled()
    assert layers["关闭标注"].labeling() is None
    assert layers["字段标注"].labelsEnabled()
    assert layers["字段标注"].labeling().settings().fieldName == "NAME"


def test_style_tools_reject_unresolved_and_wrong_resource_types_without_mutation(style_project):
    from autocarto.tools.system._shared.style_set_impl import apply_style_set_handler
    from autocarto.tools.system._shared.style_basic_impl import load_style_handler
    layer = _layer("Polygon", "area")
    before = _state(layer)
    for handler, params, code in [
        (apply_style_set_handler, {"style_set": "国标底图样式"}, "resource_handle_required"),
        (apply_style_set_handler, {"style_set": load_handle("symbols")}, "resource_type_mismatch"),
        (load_style_handler, {"layer": layer.name(), "style": load_handle("gb-basemap")}, "resource_type_mismatch"),
        (load_style_handler, {"layer": layer.name(), "path": load_handle("style")["entry"]}, "resource_handle_required"),
    ]:
        result = handler(params)
        assert result["success"] is False and result["error_type"] == code, result
        assert _state(layer) == before
