from autocarto.tests.support.resource_handles import load_handle, resolve_handle
import math
from pathlib import Path

import pytest
import yaml

from autocarto.tools.system._shared.layout_compose_impl import anchor_position, page_geometry, sparse_interval, _read_spec


@pytest.mark.parametrize("anchor,expected", [
    ("top_left", (10, 20)), ("top_center", (50, 20)), ("top_right", (90, 20)),
    ("middle_left", (10, 45)), ("center", (50, 45)), ("middle_right", (90, 45)),
    ("bottom_left", (10, 70)), ("bottom_center", (50, 70)), ("bottom_right", (90, 70)),
])
def test_layout_compose_inside_anchor(anchor, expected):
    assert anchor_position({"x": 10, "y": 20, "width": 100, "height": 60},
                           {"width": 20, "height": 10}, {"at": anchor}) == expected


@pytest.mark.parametrize("anchor,expected", [
    ("top_left", (10, 10)), ("top_center", (50, 10)), ("top_right", (90, 10)),
    ("middle_left", (-10, 45)), ("middle_right", (110, 45)),
    ("bottom_left", (10, 80)), ("bottom_center", (50, 80)), ("bottom_right", (90, 80)),
])
def test_layout_compose_outside_anchor(anchor, expected):
    assert anchor_position({"x": 10, "y": 20, "width": 100, "height": 60},
                           {"width": 20, "height": 10}, {"at": anchor, "placement": "outside"}) == expected


def test_layout_compose_anchor_offsets_and_validation():
    reference = {"x": 10, "y": 20, "width": 100, "height": 60}
    size = {"width": 20, "height": 10}
    assert anchor_position(reference, size, {"at": "top_left", "offset_mm": {"x": 3, "y": -2}}) == (13, 18)
    for anchor in ({"at": "unknown"}, {"at": "center", "placement": "outside"}, {"placement": "unknown"}, {"offset_mm": {"x": math.inf}}):
        with pytest.raises(ValueError):
            anchor_position(reference, size, anchor)


def test_layout_compose_builtin_dimensions_are_measured_from_baseline():
    route = _read_spec(load_handle("four-corner"))
    page, main, fonts, _ = page_geometry(route)
    assert page == {"width": 841, "height": 594}
    assert main == pytest.approx({"x": 63.075, "y": 58.707456, "width": 714.85, "height": 476.585088})
    assert fonts["title"] == 11.5 and fonts["legend_body"] == 2.5
    _, _, a4, _ = page_geometry(route, "A4")
    assert a4["title"] == 5.5 and a4["legend_body"] == 2
    statistics = _read_spec(load_handle("statistical"))
    page, main, fonts, _ = page_geometry(statistics)
    assert page == {"width": 297, "height": 210}
    assert main == {"x": 12, "y": 32, "width": 205, "height": 155}
    assert fonts["legend_title"] == 3.5 and fonts["legend_body"] == 2.5
    assert "border" not in statistics["elements"]


def test_layout_compose_custom_paper_and_sparse_grid():
    spec = _read_spec(load_handle("four-corner"))
    page, main, _, _ = page_geometry(spec, "300x200")
    assert page == {"width": 300, "height": 200}
    assert main["x"] == pytest.approx(22.5)
    page, _, _, _ = page_geometry(spec, "A4", "portrait")
    assert page == {"width": 210, "height": 297}
    for span in (0.001, 0.7, 8, 1000, 10000000):
        interval = sparse_interval(span)
        assert 0 < span/interval <= 4
    for paper in ("invalid", "0x100", "A5", "nanx20"):
        with pytest.raises(ValueError):
            page_geometry(spec, paper)
    with pytest.raises(ValueError):
        page_geometry(spec, "A4", "sideways")


def test_layout_compose_reads_project_relative_spec_without_cwd(tmp_path, monkeypatch):
    original = Path(__file__).resolve().parents[1]/"resources/system/layouts/四角对齐版式.layout.yaml"
    folder = tmp_path/"版式"
    folder.mkdir()
    (folder/"复制.layout.yaml").write_text(original.read_text())
    monkeypatch.setenv("AUTOCARTO_PROJECT_ROOT", str(tmp_path))
    monkeypatch.chdir(folder)
    assert _read_spec(resolve_handle(folder/"复制.layout.yaml", "layout"))["name"] == "四角对齐版式"
    bad = folder/"坏.layout.yaml"
    for raw in ("name: [", "name: 空\n", "[]"):
        bad.write_text(raw)
        with pytest.raises((ValueError, yaml.YAMLError)):
            _read_spec(resolve_handle(bad, "layout"))


def test_layout_compose_requires_the_selected_layout_format(tmp_path):
    from autocarto.tools.system._shared.resource_handle import ResourceHandleError
    with pytest.raises(ResourceHandleError) as raised:
        _read_spec(load_handle("layout-template"))
    assert raised.value.code == "resource_format_mismatch"
    with pytest.raises(ResourceHandleError, match="资源句柄"):
        _read_spec("四角对齐版式")
    source = Path(load_handle("four-corner")["entry"])
    copied = yaml.safe_load(source.read_text())
    copied["name"] = "用户版式"
    target = tmp_path / "用户版式.layout.yaml"
    target.write_text(yaml.safe_dump(copied, allow_unicode=True))
    assert _read_spec(resolve_handle(target, "layout"))["name"] == "用户版式"
