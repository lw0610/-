"""Cross-language resource contract: only production Go creates these handles."""
from pathlib import Path

import pytest

from autocarto.tests.support.resource_handles import load_handle, resolve_handle
from autocarto.tools.system._shared.resource_handle import (
    handle_entries, handle_entry_path, require_handle,
)


@pytest.mark.parametrize("fixture,kind", [
    ("data-list", "data_list"), ("data-folder", "data_folder"), ("style", "style"),
    ("style-set-modes", "style_set"), ("contract-route-xlsx", "route_style"),
    ("four-corner", "layout"), ("contract-symbols-xlsx", "symbols"), ("doc", "doc"),
])
def test_contract_all_eight_types_use_complete_go_handles(fixture, kind):
    handle = require_handle(load_handle(fixture), kind)
    assert handle["truncated"] is False
    assert handle["total_entries"] == len(handle_entries(handle))
    assert Path(handle["root"]).is_absolute()
    if kind != "data_folder":
        assert handle_entry_path(handle).is_file()
    for entry in handle_entries(handle):
        if kind == "route_style":
            assert entry["path"] == ""
        else:
            assert Path(entry["path"]).is_absolute()
            assert Path(entry["path"]).is_file() == entry["exists"]


def test_contract_instruction_sheet_and_aliases_reach_consumers_unchanged():
    from autocarto.knowledge.visual_presets import load_route_styles, load_symbol_styles
    from autocarto.tools.system._shared.style_set_support import matching_style

    data = load_handle("contract-data-xlsx")
    assert [(row["name"], row["kind"], row["required"]) for row in data["entries"]] == [("契约面", "vector", True)]
    styles = load_handle("contract-style-xlsx")
    selected = matching_style(styles["entries"], "区域")
    assert selected == styles["entries"][0]
    assert selected["label_mode"] == "field" and selected["label_field"] == "NAME"
    assert selected["style_path"] == selected["path"]

    route = load_handle("contract-route-xlsx")
    presets, defaults = load_route_styles(route)
    assert list(presets.values()) == route["entries"]
    for row in route["entries"]:
        assert row["reference_style_id"] == "route_blue_reference"
        assert row["width_scale"] == 1.25
        assert row["label_paths"] is True and row["auto_width_group"] is False
        for side in ("friendly", "enemy", "neutral"):
            assert defaults[row["topology"], side] == row["preset_ref"]

    symbols = load_handle("contract-symbols-xlsx")
    presets, events, sources, rows = load_symbol_styles(symbols)
    assert rows == symbols["entries"]
    assert list(sources) == [row["symbol_type"] for row in rows]
    assert list(sources.values()) == [Path(row["path"]) for row in rows]
    for row in rows:
        for key in ("size", "priority", "anchor_x", "anchor_y"):
            assert presets[row["symbol_type"]][key] == row[key]
    assert events == {"出发": "start_flag", "抵达": "waypoint", "会合": "waypoint"}


@pytest.mark.parametrize("kind,filename,content", [
    ("data_list", "list.csv", "name\nmissing_path\n"),
    ("data_folder", "folder.geojson", "{}"),
    ("style", "single.qml", "<svg/>"),
    ("style_set", "set/mapping.csv", "layer\narea\n"),
    ("route_style", "route.csv", "topology,side\nstandard,friendly\n"),
    ("layout", "map.qpt", "<Layout/>"),
    ("symbols", "points/mapping.csv", "symbol_type\nwaypoint\n"),
    ("doc", "document.pdf", "invalid PDF"),
])
def test_contract_error_resources_never_cross_the_go_python_boundary(tmp_path, kind, filename, content):
    path = tmp_path / filename
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(content, encoding="utf-8")
    if kind in {"style_set", "symbols"}:
        path = path.parent
    with pytest.raises(ValueError, match="FLOW_RESOURCE_INVALID"):
        resolve_handle(path, kind)


@pytest.mark.parametrize("kind,extension,header,xml", [
    ("style_set", "qml", "layer,style_file", "<qgis/>"),
    ("symbols", "svg", "symbol_type,SVG路径", "<svg/>"),
])
@pytest.mark.parametrize("path_type", ["outside", "absolute", "symlink"])
def test_contract_mapped_paths_cannot_escape_into_handles(tmp_path, kind, extension, header, xml, path_type):
    source = tmp_path / ("outside." + extension)
    source.write_text(xml, encoding="utf-8")
    folder = tmp_path / "set"
    folder.mkdir()
    path = "../" + source.name
    if path_type == "absolute":
        path = str(source)
    elif path_type == "symlink":
        path = "link." + extension
        (folder / path).symlink_to(source)
    (folder / "mapping.csv").write_text(header + "\na," + path + "\n", encoding="utf-8")
    with pytest.raises(ValueError, match="FLOW_RESOURCE_INVALID"):
        resolve_handle(folder, kind)


def test_contract_required_and_optional_shapefile_sidecars(tmp_path):
    (tmp_path / "area.shp").touch()
    (tmp_path / "area.shx").touch()
    table = tmp_path / "list.csv"
    table.write_text("name,path,required\narea,area.shp,true\n", encoding="utf-8")
    with pytest.raises(ValueError, match="FLOW_DATA_SIDECAR_MISSING"):
        resolve_handle(table, "data_list")
    table.write_text("name,path,required\narea,area.shp,false\n", encoding="utf-8")
    handle = resolve_handle(table, "data_list")
    assert handle["entries"][0]["sidecars_missing"] == [".dbf", ".prj"]
    assert len([row for row in handle["warnings"] if row["code"] == "FLOW_DATA_SIDECAR_MISSING"]) == 2


def test_contract_doc_reads_exact_delivered_file():
    handle = require_handle(load_handle("doc"), "doc", formats=("md",))
    assert handle_entry_path(handle).read_text(encoding="utf-8").startswith("#")
