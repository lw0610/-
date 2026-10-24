"""资源句柄边界校验；不负责资源查找或表格解析。"""

from copy import deepcopy

import pytest

from autocarto.tools.system._shared.resource_handle import (
    ResourceHandleError, handle_entries, handle_entry_path, handle_summary, require_handle,
)


def _document(path):
    # 合同边界的最小合成消息，无资源表或文件解析。
    return {"api_version": "resource-handle/v1", "id": "doc.example", "type": "doc",
            "name": "示例说明", "version": "1.0.0", "entry": str(path), "format": "md",
            "entries": [], "total_entries": 0, "truncated": False}


@pytest.mark.parametrize("value", [None, "资源名称", "/path/to/resource.csv", []])
def test_resource_handle_rejects_unresolved_inputs(value):
    with pytest.raises(ResourceHandleError) as raised:
        require_handle(value, "data_list")
    assert raised.value.code == "resource_handle_required"


def test_resource_handle_reads_contract_without_opening_resource(tmp_path):
    value = _document(tmp_path / "not-read.md")
    before = deepcopy(value)
    assert require_handle(value, "doc", formats=("md", "txt")) is value
    assert handle_entry_path(value) == tmp_path / "not-read.md"
    assert handle_entries(value) == []
    assert handle_summary(value) == {"id": "doc.example", "name": "示例说明", "version": "1.0.0"}
    assert value == before


@pytest.mark.parametrize("changes,expected", [
    ({"api_version": "unknown"}, "resource_handle_invalid"),
    ({"type": "symbols"}, "resource_type_mismatch"),
    ({"format": "pdf"}, "resource_format_mismatch"),
    ({"id": ""}, "resource_handle_invalid"),
    ({"truncated": True}, "resource_handle_incomplete"),
    ({"total_entries": 1}, "resource_handle_incomplete"),
    ({"entries": [{}]}, "resource_handle_invalid"),
])
def test_resource_handle_rejects_incomplete_or_incompatible_contract(tmp_path, changes, expected):
    value = {**_document(tmp_path / "example.md"), **changes}
    with pytest.raises(ResourceHandleError) as raised:
        require_handle(value, "doc", formats=("md",))
    assert raised.value.code == expected


@pytest.mark.parametrize("path", ["relative.md", "", "bad\x00.md"])
def test_resource_handle_does_not_resolve_relative_paths(tmp_path, path):
    value = {**_document(tmp_path / "example.md"), "entry": path}
    with pytest.raises(ResourceHandleError) as raised:
        handle_entry_path(value)
    assert raised.value.code == "resource_path_invalid"


def test_legend_extra_selects_normalized_symbol_and_rejects_ambiguous_input():
    from autocarto.tests.support.resource_handles import load_handle
    from autocarto.tools.system._shared.resource_handle import legend_extra_spec
    symbols = load_handle("route-arrow")
    path, size, label = legend_extra_spec({"label": "路线方向", "symbol_type": "route_arrow"}, symbols)
    assert str(path) == symbols["entries"][0]["path"]
    assert size == symbols["entries"][0]["size"] and label == "路线方向"
    for extra in ({"label": "方向", "symbol_type": "route_arrow", "image_path": str(path)},
                  {"label": "方向", "symbol_type": "missing"},
                  {"label": "方向", "symbol_type": "route_arrow", "size_mm": float("nan")},
                  {"label": "方向"}):
        with pytest.raises(ValueError):
            legend_extra_spec(extra, symbols)
    with pytest.raises(ResourceHandleError):
        legend_extra_spec({"label": "方向", "symbol_type": "route_arrow"})
