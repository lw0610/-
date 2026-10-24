import copy
import csv
import json
from pathlib import Path

import pytest

from .compare_golden import compare_directories, compare_snapshots


def sample():
    return {"schema_version": "autocarto-golden/v1", "elements": {"title": {
        "type": "QgsLayoutItemLabel", "rect_mm": {"x": 10, "y": 20, "width": 80, "height": 12},
        "font": {"size": 5.5, "unit": 1}}}, "main_map": {"layers": ["values", "background"]},
        "layers": {"values": {"renderer_sha256": "abc"}}, "metadata": {"qgis_version": "test"}}


def test_millimeter_tolerance_does_not_hide_font_layer_order_or_renderer_changes():
    expected = sample()
    candidate = copy.deepcopy(expected)
    candidate["elements"]["title"]["rect_mm"]["x"] += 0.9
    candidate["metadata"]["qgis_version"] = "different machine"
    assert compare_snapshots(expected, candidate) == []
    candidate["elements"]["title"]["font"]["size"] = 6
    candidate["main_map"]["layers"].reverse()
    candidate["layers"]["values"]["renderer_sha256"] = "changed"
    assert {item["path"] for item in compare_snapshots(expected, candidate)} == {
        "elements/title/font/size", "main_map/layers", "layers/values/renderer_sha256"}


def test_missing_elements_and_excess_position_drift_fail():
    candidate = sample()
    candidate["elements"]["title"]["rect_mm"]["y"] += 1.001
    assert compare_snapshots(sample(), candidate)[0]["path"] == "elements/title/rect_mm/y"
    candidate["elements"] = {}
    assert compare_snapshots(sample(), candidate)[0]["path"] == "elements/title"


def test_ignore_configuration_is_scoped_to_the_requested_fields():
    candidate = sample()
    candidate["layers"]["values"]["renderer_sha256"] = "new"
    assert compare_snapshots(sample(), candidate, ignore=["layers/*/renderer_sha256"]) == []
    candidate["elements"]["extra"] = {"type": "QgsLayoutItemLabel"}
    assert compare_snapshots(sample(), candidate, ignore=["layers/*/renderer_sha256"])
    with pytest.raises(ValueError):
        compare_snapshots(sample(), candidate, tolerance_mm=float("nan"))


def test_preview_difference_is_reported_without_failing_structure(tmp_path):
    Image = pytest.importorskip("PIL.Image")
    directories = [tmp_path / "baseline", tmp_path / "candidate"]
    for index, directory in enumerate(directories):
        directory.mkdir()
        (directory / "snapshot.json").write_text(json.dumps(sample()), encoding="utf-8")
        pixels = bytes((column * 28 if index == 0 else 255 - column * 28)
                       for _ in range(8) for column in range(9))
        Image.frombytes("L", (9, 8), pixels).save(directory / "preview.png")
    comparison = compare_directories(*directories)
    assert comparison["success"] is True
    assert comparison["preview"]["distance"] == 64
    assert comparison["preview"]["diagnostic_only"] is True


def test_route_fixture_is_portable_and_retains_validated_table_counts():
    fixture = Path(__file__).parent / "fixtures" / "route_dataset"
    manifest = json.loads((fixture / "compile_manifest.json").read_text(encoding="utf-8"))
    assert json.loads((fixture / "validation_report.json").read_text(encoding="utf-8"))["is_valid"] is True
    for filename, expected_count in manifest["counts"].items():
        with (fixture / filename).open(encoding="utf-8-sig", newline="") as stream:
            rows = list(csv.DictReader(stream))
        assert len(rows) == expected_count
        for row in rows:
            for key in ("image_path", "svg_path"):
                if row.get(key):
                    assert not Path(row[key]).is_absolute()
                    assert (fixture / row[key]).is_file()


def test_statistical_fixture_contains_real_regions_and_explicit_edge_cases():
    fixture = Path(__file__).parent / "fixtures" / "statistical"
    regions = json.loads((fixture / "regions.geojson").read_text(encoding="utf-8"))["features"]
    assert len(regions) == 17
    assert all(str(row["properties"]["adcode"]).startswith("42") for row in regions)
    with (fixture / "values.csv").open(encoding="utf-8-sig", newline="") as stream:
        rows = list(csv.DictReader(stream))
    target = next(row for row in rows if row["年份"] == "2020")
    names = [row["properties"]["name"] for row in regions]
    assert sum(target[name] == "" for name in names) == 1
    assert target[names[0]] == "0"
    assert "未匹配示例区" not in names and target["未匹配示例区"] == "999"
