"""离线边界来源与精确身份选择，不要求 QGIS 或 GeoPandas。"""

import hashlib
import json
from pathlib import Path

import pytest

from autocarto.tools.system._shared import admin_boundary_impl as boundary


def write_boundary_asset(root, name="示例省", code="120000", geometry=None):
    root.mkdir(parents=True, exist_ok=True)
    feature = {"type": "Feature", "properties": {"name": name, "adcode": code, "gb": "156" + code, "level": "province"},
               "geometry": geometry or {"type": "Polygon", "coordinates": [[[110, 30], [111, 30], [111, 31], [110, 31], [110, 30]]]}}
    document = {"type": "FeatureCollection", "crs": {"type": "name", "properties": {"name": "urn:ogc:def:crs:OGC:1.3:CRS84"}}, "features": [feature]}
    raw = json.dumps(document, ensure_ascii=False).encode("utf-8")
    path = root / "china_province.geojson"
    path.write_bytes(raw)
    manifest = {"repository": "https://example.invalid/source", "commit": "a" * 40,
                "assets": [{"local_path": "data/china_province.geojson", "bytes": len(raw), "sha256": hashlib.sha256(raw).hexdigest(), "feature_count": 1}]}
    (root / "upstream-manifest.json").write_text(json.dumps(manifest), encoding="utf-8")
    return path, feature


def test_admin_boundary_packaged_sources_match_manifest_and_identity_counts():
    for level, count in (("province", 34), ("city", 375), ("county", 2891)):
        features, record, path = boundary._load_boundary_features(level)
        assert len(features) == count
        assert hashlib.sha256(path.read_bytes()).hexdigest() == record["asset"]["sha256"]


def test_admin_boundary_ambiguous_name_returns_candidates_without_output(tmp_path):
    result = boundary.extract_boundary_handler({"level": "县", "name": "长安区", "output": str(tmp_path / "result.geojson")})
    assert result["success"] is False
    assert result["error_type"] == "boundary_ambiguous"
    assert {row["adcode"] for row in result["data"]["candidates"]} == {"610116", "130102"}
    assert not list(tmp_path.iterdir())
    selected, _, _ = boundary._select_boundary({"level": "county", "name": "长安区", "code": "610116"})
    assert selected["properties"]["adcode"] == "610116"


@pytest.mark.parametrize("params", [
    {"level": "country", "name": "示例"}, {"level": "省"},
    {"level": "省", "code": 120000}, {"level": "省", "code": "12"},
    {"level": "省", "name": ""},
])
def test_admin_boundary_invalid_identity_is_rejected_before_io(params):
    result = boundary.extract_boundary_handler(params)
    assert result["success"] is False
    assert result["error_type"] == "invalid_arguments"


def test_admin_boundary_tool_assets_only_and_source_integrity(tmp_path, monkeypatch):
    monkeypatch.setattr(boundary, "tool_asset_path", lambda domain, name: tmp_path / "tools/system" / domain / "assets" / name)
    legacy = tmp_path / "skills/system/china-admin-boundaries"
    old_path, _ = write_boundary_asset(legacy, name="旧示例省")
    result = boundary.extract_boundary_handler({"level": "省", "code": "120000"})
    assert result["error_type"] == "boundary_source_missing"
    assert old_path.is_file()
    resource = tmp_path / "tools/system/admin-boundaries/assets/中国行政区边界"
    new_path, feature = write_boundary_asset(resource)
    chosen, _, path = boundary._select_boundary({"level": "省", "code": "120000"})
    assert chosen == feature
    assert path == new_path
    new_path.write_bytes(new_path.read_bytes() + b" ")
    result = boundary.extract_boundary_handler({"level": "province", "code": "120000"})
    assert result["error_type"] == "boundary_source_invalid"
    assert result["success"] is False


def test_admin_boundary_missing_columns_are_not_invented(tmp_path, monkeypatch):
    monkeypatch.setattr(boundary, "tool_asset_path", lambda domain, name: tmp_path / "tools/system" / domain / "assets" / name)
    resource = tmp_path / "tools/system/admin-boundaries/assets/中国行政区边界"
    path, _ = write_boundary_asset(resource)
    document = json.loads(path.read_text())
    del document["features"][0]["properties"]["adcode"]
    raw = json.dumps(document).encode("utf-8")
    path.write_bytes(raw)
    manifest_path = resource / "upstream-manifest.json"
    manifest = json.loads(manifest_path.read_text())
    manifest["assets"][0].update(bytes=len(raw), sha256=hashlib.sha256(raw).hexdigest())
    manifest_path.write_text(json.dumps(manifest))
    result = boundary.extract_boundary_handler({"level": "省", "code": "120000"})
    assert result["success"] is False
    assert result["error_type"] == "boundary_source_invalid"


def test_admin_boundary_all_packaged_geometries_and_upstream_blobs_are_intact():
    from shapely.geometry import shape
    root, _ = boundary._boundary_asset("province")
    manifest = json.loads((root / "upstream-manifest.json").read_text())
    for asset in manifest["assets"]:
        raw = (root / asset["local_path"]).read_bytes()
        assert len(raw) == asset["bytes"]
        assert hashlib.sha256(raw).hexdigest() == asset["sha256"]
        assert hashlib.sha1(("blob " + str(len(raw)) + "\0").encode() + raw).hexdigest() == asset["git_blob_sha1"]
    for level in ("province", "city", "county"):
        features, _, _ = boundary._load_boundary_features(level)
        for feature in features:
            geometry = shape(feature["geometry"])
            assert not geometry.is_empty and geometry.is_valid, feature["properties"]["adcode"]


def test_admin_boundary_wuhan_identity_and_geometry_are_preserved(tmp_path):
    feature, _, _ = boundary._select_boundary({"level": "city", "name": "武汉市"})
    assert feature["properties"]["adcode"] == "420100"
    assert feature["geometry"]["type"] == "MultiPolygon"
    import importlib.util
    worker_path = Path(__file__).resolve().parents[2] / "carto_agent/internal/geeruntime/worker.py"
    spec = importlib.util.spec_from_file_location("gee_boundary_reader", worker_path)
    worker = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(worker)
    # The generic region reader accepts exactly this original geometry.
    exported = tmp_path / "wuhan.geojson"
    exported.write_text(json.dumps({"type": "FeatureCollection", "features": [feature]}))
    assert worker.read_region_geometry(str(exported)) == feature["geometry"]


@pytest.mark.parametrize("parts", [("../outside",), ("/outside",), ("nested/../../outside",), ("C:\\outside",)])
def test_tool_assets_reject_paths_outside_private_directory(parts):
    from autocarto.tools.system._shared.tool_assets import tool_asset_path
    with pytest.raises(ValueError):
        tool_asset_path("admin-boundaries", *parts)
