"""Real QGIS export -> public library import -> new flow -> QGIS reuse.

The role/model adapter is deterministic. This covers production library, plan validation,
controller and Python tools; live capture HTTP, UI and online models are separate
contracts, not assertions made by this harness. All state belongs to --out.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[4]
LAYER = "捕获验收区域"
COLOR = "#207f91"
TITLE = "工程模板复用验收"
LAYOUT = "捕获后新流程版面"


def write_json(path, data):
    path.write_text(json.dumps(data, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")


def hashes(directory):
    return {str(path.relative_to(directory)): hashlib.sha256(path.read_bytes()).hexdigest()
            for path in directory.rglob("*") if path.is_file()}


def capture(out):
    sys.path.insert(0, str(ROOT))
    from autocarto.tests.golden.qgis_bootstrap import isolated_qgis
    with isolated_qgis(out / "capture-process"):
        import yaml
        from qgis.PyQt.QtGui import QColor, QFont
        from qgis.core import (QgsCoordinateReferenceSystem, QgsLayoutItemLabel, QgsLayoutItemMap,
                               QgsLayoutPoint, QgsLayoutSize, QgsPalLayerSettings, QgsPrintLayout,
                               QgsProject, QgsTextFormat, QgsVectorLayer, QgsVectorLayerSimpleLabeling)
        from autocarto.core.tool_engine.definitions import ToolDefinitionLoader
        from autocarto.core.tool_engine.executor import ProcessingToolExecutor
        project = QgsProject.instance()
        project.setCrs(QgsCoordinateReferenceSystem("EPSG:4326"))
        source = out / "sources" / "区域.geojson"
        layer = QgsVectorLayer(str(source), LAYER, "ogr")
        assert layer.isValid()
        layer.renderer().symbol().setColor(QColor(COLOR))
        label = QgsPalLayerSettings()
        label.fieldName = "name"
        layer.setLabeling(QgsVectorLayerSimpleLabeling(label))
        layer.setLabelsEnabled(True)
        project.addMapLayer(layer)
        layout = QgsPrintLayout(project)
        layout.initializeDefaults()
        layout.setName("原始工程版面")
        map_item = QgsLayoutItemMap(layout)
        map_item.setId("original_map")
        layout.addLayoutItem(map_item)
        map_item.attemptMove(QgsLayoutPoint(12, 30))
        map_item.attemptResize(QgsLayoutSize(180, 150))
        map_item.setCrs(layer.crs())
        map_item.setLayers([layer])
        map_item.zoomToExtent(layer.extent())
        title = QgsLayoutItemLabel(layout)
        title.setId("title")
        title.setText("{{标题}}")
        font = QFont("Arial", 16)
        font.setBold(True)
        fmt = QgsTextFormat()
        fmt.setFont(font)
        fmt.setSize(16)
        title.setTextFormat(fmt)
        title.setCustomProperty("autocarto/font_locked", True)
        layout.addLayoutItem(title)
        title.attemptMove(QgsLayoutPoint(12, 8))
        title.attemptResize(QgsLayoutSize(180, 15))
        project.layoutManager().addLayout(layout)
        project.setDirty(False)
        exported = {}
        for domain, name, params in [
            ("print-layout", "layout_save_template", {"layout": layout.name(), "path": str(out / "captured" / "版式.qpt")}),
            ("cartography", "layer_save_style", {"layer": layer.id(), "path": str(out / "captured" / "样式.qml")}),
        ]:
            path = ROOT / "autocarto/tools/system" / domain / (name + ".yaml")
            definition = yaml.safe_load(path.read_text())
            assert definition.get("policy", {}).get("visibility", "public") == "public"
            assert "tool.invoke" in definition.get("policy", {}).get("authorization_scopes", ["tool.invoke"])
            module, function = definition["execution"]["handler"].rsplit(".", 1)
            loader = ToolDefinitionLoader([{
                "name": name, "domain": domain, "runtime": "qgis", "parameters": definition["spec"]["parameters"],
                "handler_file": str(path.parent / Path(*module.split(".")).with_suffix(".py")), "handler_function": function,
            }])
            result = ProcessingToolExecutor(loader.load_all()).execute(name, params)
            assert result["success"], result
            exported[name] = result
        assert map_item.id() == "original_map" and title.text() == "{{标题}}"
        assert not project.isDirty() and len(project.layoutManager().layouts()) == 1
        assert exported["layout_save_template"]["data"]["map_items"][0]["id"] == "main_map"
        assert exported["layout_save_template"]["data"]["placeholders"] == ["标题"]
        assert Path(exported["layer_save_style"]["output"]).is_file()
        write_json(out / "capture-results.json", {"tools": exported, "source_project_unchanged": True})


def prepare(out):
    sources = out / "sources"
    sources.mkdir()
    write_json(sources / "区域.geojson", {"type": "FeatureCollection", "features": [
        {"type": "Feature", "properties": {"name": "甲区"}, "geometry": {"type": "Polygon", "coordinates": [[[110, 30], [112, 30], [112, 32], [110, 32], [110, 30]]]}}
    ]})
    package = out / "new-flow"
    package.mkdir()
    (package / "FLOW.md").write_text('''---
编号: captured-project-roundtrip
名称: 工程捕获资源复用验收
说明: 导入真实工程保存的版面和单个样式，在新流程中复用。
版本: 2.0.0
---

只读加载用户给的区域文件，核验name字段。对区域图层套用样式「工程捕获样式」，按name字段标注，并读取实际颜色核查。

按版面「工程捕获版式」排版，主地图显示区域图层。把标题占位符替换成用户标题，核验名称和主地图项。

导出PNG和PDF，保持源数据和原始工程不变。
''', encoding="utf-8")
    recipe = {
        "id": "capture-roundtrip", "flow_id": "captured-project-roundtrip", "flow_package": str(package),
        "resource_imports": [{"source": str(out / "captured" / "版式.qpt"), "type": "layout", "name": "工程捕获版式"},
                             {"source": str(out / "captured" / "样式.qml"), "type": "style", "name": "工程捕获样式"}],
        "project_crs": "EPSG:4326", "layout": LAYOUT, "variables": {},
        "inputs": {"区域数据": str(sources / "区域.geojson"), "地图标题": TITLE},
        "steps": {
            "s1": {"actions": [{"tool": "data_load_vector", "params": {"path": {"$ref": "inputs.区域数据"}, "layer_name": LAYER}},
                                {"op": "layers", "names": [LAYER], "save": "layers"}],
                   "outputs": {"区域图层": {"$ref": "layers"}}},
            "s2": {"actions": [{"tool": "layer_load_style", "params": {"style": "工程捕获样式", "layer": LAYER, "label_field": "name"}, "save": "applied"},
                                {"tool": "layer_get_style_info", "params": {"layer": LAYER}, "save": "actual_style"}],
                   "outputs": {"样式结果": {"$json": "actual_style"}}},
            "s3": {"actions": [{"tool": "import_layout_template", "params": {"template": "工程捕获版式", "name": LAYOUT,
                                  "texts": {"标题": {"$ref": "inputs.地图标题"}}, "map_layers": [LAYER], "extent_layers": [LAYER]}, "save": "imported_template"}],
                   "outputs": {"版面": LAYOUT}},
            "s4": {"actions": [{"tool": "export_print_layout", "params": {"layout": LAYOUT, "output": "${out}/捕获复用.png", "dpi": 72}, "save": "png"},
                                {"tool": "export_layout_pdf", "params": {"layout": LAYOUT, "output": "${out}/捕获复用.pdf", "dpi": 72}, "save": "pdf"}],
                   "outputs": {"成果 PNG": {"$ref": "png.output"}, "成果 PDF": {"$ref": "pdf.output"}}},
        },
        "invariants": {"style": {"$ref": "actual_style.data"}, "applied": {"$ref": "applied.data"}, "template": {"$ref": "imported_template"}},
    }
    write_json(out / "scenario-source.json", recipe)


def verify(out, before):
    run = out / "run" / "capture-roundtrip"
    snap = json.loads((run / "snapshot.json").read_text())
    assert snap["invariants"]["style"]["symbol_color"] == COLOR
    assert snap["layers"][LAYER]["label_field"] == "name" and snap["layers"][LAYER]["labels_enabled"]
    assert snap["main_map"]["layers"] == [LAYER]
    assert snap["elements"]["title"]["text"] == TITLE
    assert "main_map" in snap["elements"] and "original_map" not in snap["elements"]
    variables = json.loads((run / "adapter-variables.json").read_text())
    assert variables["imported_template"]["data"]["unreplaced_placeholders"] == []
    for key, prefix in [("png", b"\x89PNG"), ("pdf", b"%PDF")]:
        path = Path(variables[key]["output"]).resolve()
        assert path.is_relative_to(run.resolve())
        content = path.read_bytes()
        assert content.startswith(prefix) and len(content) > 500
    imports = json.loads((run / "imported-resources.json").read_text())
    assert {item["type"] for item in imports} == {"layout", "style"}
    assert all(item["owner"] == "library_user" for item in imports), imports
    plan = json.loads((run / "plan.json").read_text())
    assert len(plan["resources"]) == 2
    assert all(item["owner"] == "library_user" for item in plan["resources"]), plan["resources"]
    lock = run / "controller-runtime/flows/captured-project-roundtrip/flow.lock"
    assert lock.is_file() and "工程捕获" in lock.read_text()
    assert {item["id"] for item in imports} == {item["id"] for item in plan["resources"]}
    for item in imports:
        assert item["id"] in lock.read_text() and item["digest"] in lock.read_text()
    # Verify tool execution consumed the Go resolver's library copies, not the
    # original export paths or a recipe-built imitation of a resource handle.
    calls = json.loads((run / "tool-calls.json").read_text())
    for name, parameter, kind in [("layer_load_style", "style", "style"),
                                  ("import_layout_template", "template", "layout")]:
        call = next(item for item in calls if item["tool"] == name)
        handle = call["params"][parameter]
        imported = next(item for item in imports if item["type"] == kind)
        assert handle["id"] == imported["id"] and handle["digest"] == imported["digest"]
        assert Path(handle["root"]).is_relative_to(run / "controller-runtime/resources")
    after = {"sources": hashes(out / "sources"), "captured": hashes(out / "captured")}
    assert before == after, "导入/执行修改了源数据或工具导出物"
    report = {"success": True, "source_project_unchanged": True, "source_files_unchanged": True,
              "pipeline": ["actual PyQGIS tool exports", "Library.ImportNamed", "Store.Import", "PlanExec.Build", "Controller.StartFromPlan", "actual QGIS resource-handle consumption", "PNG/PDF export"],
              "not_covered": ["live capture HTTP transport", "interactive UI", "online model decisions"],
              "source_sha256": before, "output": str(run), "resource_ids": [item["id"] for item in imports],
              "rendered_color": COLOR, "label_field": "name", "rendered_title": TITLE}
    write_json(out / "roundtrip-report.json", report)
    print(json.dumps(report, ensure_ascii=False, indent=2))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--out", type=Path, required=True)
    parser.add_argument("--capture", action="store_true", help=argparse.SUPPRESS)
    args = parser.parse_args()
    out = args.out.resolve()
    os.environ.update(AUTOCARTO_PROJECT_ROOT=str(ROOT), AUTOCARTO_ASSETS_ROOT=str(ROOT / "autocarto"),
                      AUTOCARTO_HOME=str(out / "runtime"), AUTOCARTO_DISABLE_KEYRING="1", QT_QPA_PLATFORM="offscreen",
                      PYTHONDONTWRITEBYTECODE="1")
    if args.capture:
        capture(out)
        return
    out.mkdir(parents=True, exist_ok=False)
    prepare(out)
    subprocess.run([sys.executable, str(Path(__file__).resolve()), "--out", str(out), "--capture"], check=True)
    before = {"sources": hashes(out / "sources"), "captured": hashes(out / "captured")}
    subprocess.run([sys.executable, str(Path(__file__).with_name("run.py")), "--out", str(out / "run"), str(out / "scenario-source.json")], check=True)
    verify(out, before)


if __name__ == "__main__":
    main()
