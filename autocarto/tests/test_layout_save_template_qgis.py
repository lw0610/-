"""真实 QGIS：版式保存修改序列化副本，工程对象及脏状态保持不变。"""
from pathlib import Path
import xml.etree.ElementTree as ET

import pytest

from autocarto.tests.golden.qgis_bootstrap import isolated_qgis, qgis_available


@pytest.fixture(scope="module")
def qgis_runtime(tmp_path_factory):
    if not qgis_available():
        pytest.skip("需要 QGIS Python")
    with isolated_qgis(tmp_path_factory.mktemp("template-save-runtime")):
        yield


@pytest.fixture
def project(qgis_runtime):
    from qgis.core import QgsProject
    current = QgsProject.instance()
    current.clear()
    yield current
    current.clear()


def _layout(project, identifiers):
    from qgis.core import QgsPrintLayout, QgsLayoutItemMap, QgsLayoutItemLabel
    layout = QgsPrintLayout(project)
    layout.initializeDefaults()
    layout.setName("可复用版面")
    for identifier in identifiers:
        item = QgsLayoutItemMap(layout)
        item.setId(identifier)
        layout.addLayoutItem(item)
    label = QgsLayoutItemLabel(layout)
    label.setId("title")
    label.setText("{{标题}} / {{ 数据来源 }} / {{标题}}")
    layout.addLayoutItem(label)
    project.layoutManager().addLayout(layout)
    project.setDirty(False)
    return layout


def _invoke(params):
    import yaml
    from autocarto.core.tool_engine.definitions import ToolDefinitionLoader
    from autocarto.core.tool_engine.executor import ProcessingToolExecutor
    path = Path(__file__).resolve().parents[1] / "tools/system/print-layout/layout_save_template.yaml"
    definition = yaml.safe_load(path.read_text())
    assert definition["policy"]["visibility"] == "public"
    assert "tool.invoke" in definition["policy"]["authorization_scopes"]
    module, function = definition["execution"]["handler"].rsplit(".", 1)
    loader = ToolDefinitionLoader([{
        "name": "layout_save_template", "domain": "print-layout", "runtime": "qgis",
        "parameters": definition["spec"]["parameters"],
        "handler_file": str(path.parent / Path(*module.split(".")).with_suffix(".py")),
        "handler_function": function,
    }])
    return ProcessingToolExecutor(loader.load_all()).execute("layout_save_template", params)


def test_single_map_export_keeps_live_project_and_placeholder_values(project, tmp_path):
    from qgis.core import QgsLayoutItemMap
    layout = _layout(project, ["map_original"])
    original = next(item for item in layout.items() if isinstance(item, QgsLayoutItemMap))
    output = tmp_path / "保存.qpt"
    result = _invoke({"layout": layout.name(), "path": str(output)})
    assert result["success"], result
    assert original.id() == "map_original"
    assert not project.isDirty()
    assert len(project.layoutManager().layouts()) == 1
    root = ET.parse(output).getroot()
    assert [item for item in root.iter("LayoutItem") if item.get("id") == "main_map"]
    assert result["data"]["path"] == str(output)
    assert result["data"]["map_items"][0]["id"] == "main_map"
    assert result["data"]["placeholders"] == ["数据来源", "标题"]
    assert layout.itemById("title").text() == "{{标题}} / {{ 数据来源 }} / {{标题}}"


def test_multiple_maps_require_selection_and_rename_only_selected_copy(project, tmp_path):
    from autocarto.tools.system._shared.layout_template_save_impl import save_layout_template_handler
    layout = _layout(project, ["main_map", "inset"])
    output = tmp_path / "multi.qpt"
    params = {"layout": layout.name(), "path": str(output)}
    failure = save_layout_template_handler(params)
    assert not failure["success"]
    assert failure["error_type"] == "main_map_required"
    assert {item["id"] for item in failure["data"]["map_items"]} == {"main_map", "inset"}
    assert not output.exists()
    selected_uuid = layout.itemById("inset").uuid()
    result = save_layout_template_handler(dict(params, main_map_item=selected_uuid))
    assert result["success"], result
    exported = {item["uuid"]: item["id"] for item in result["data"]["map_items"]}
    assert exported[selected_uuid] == "main_map"
    assert exported[layout.itemById("main_map").uuid()] == "main_map_2"
    assert layout.itemById("inset") is not None
    assert not project.isDirty()


def test_layout_list_exposes_unnamed_map_selectors_and_empty_list(project):
    from autocarto.tools.system._shared.print_layout_impl import list_layouts_handler
    assert list_layouts_handler({})["data"]["layouts"] == []
    layout = _layout(project, ["", "another"])
    result = list_layouts_handler({})
    assert result["success"], result
    row = result["data"]["layouts"][0]
    assert row["name"] == layout.name()
    assert len(row["map_items"]) == 2
    assert all(item["uuid"] for item in row["map_items"])


@pytest.mark.parametrize("params", [{"layout": "missing"}, {"path": "bad.txt"}, {"main_map_item": "missing"}])
def test_invalid_selection_never_writes_output(project, tmp_path, params):
    from autocarto.tools.system._shared.layout_template_save_impl import save_layout_template_handler
    layout = _layout(project, ["map"])
    output = tmp_path / "absent.qpt"
    result = save_layout_template_handler({"layout": layout.name(), "path": str(output), **params})
    assert not result["success"]
    assert not output.exists()
    assert not project.isDirty()
