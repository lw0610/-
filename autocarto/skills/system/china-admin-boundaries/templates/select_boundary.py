"""Select one verified GeoCode China administrative polygon, without network I/O.

Upstream data and license are preserved beside this local adapter. This module
uses the packaged data; it does not geocode a name into an invented rectangle.
"""

import argparse
import hashlib
import json
import math
import os
from pathlib import Path
import re
import tempfile


PACKAGE_DIR = Path(__file__).resolve().parents[1]
SOURCE_COMMIT = "6e3534f7af3675b19bd3c5d54c94022959dccdd2"
LEVELS = ("province", "city", "county")
CRS84 = "urn:ogc:def:crs:OGC:1.3:CRS84"


class BoundarySelectionError(ValueError):
    def __init__(self, code, message, candidates=None):
        super().__init__(message)
        self.code = code
        self.candidates = candidates or []


def _manifest():
    value = json.loads((PACKAGE_DIR / "upstream-manifest.json").read_text(encoding="utf-8"))
    if value.get("commit") != SOURCE_COMMIT:
        raise BoundarySelectionError("boundary_source_invalid", "Packaged boundary source commit is unexpected")
    return value


def _validate_layer(value, level):
    if not isinstance(value, dict):
        raise BoundarySelectionError("boundary_source_invalid", "Boundary asset must be a GeoJSON object")
    crs = value.get("crs", {})
    if not isinstance(crs, dict) or crs.get("type") != "name" or crs.get("properties", {}).get("name") != CRS84:
        raise BoundarySelectionError("boundary_source_invalid", "Expected the original WGS84 longitude/latitude CRS84 declaration; do not relabel GCJ-02 coordinates")
    features = value.get("features")
    if value.get("type") != "FeatureCollection" or not isinstance(features, list) or not features:
        raise BoundarySelectionError("boundary_source_invalid", "Boundary asset must be a nonempty FeatureCollection")
    seen = set()
    for feature in features:
        if not isinstance(feature, dict):
            raise BoundarySelectionError("boundary_source_invalid", "Boundary feature must be a GeoJSON object")
        props = feature.get("properties", {})
        if not isinstance(props, dict) or not isinstance(feature.get("geometry"), dict):
            raise BoundarySelectionError("boundary_source_invalid", "Boundary feature lacks attributes or polygon geometry")
        adcode = props.get("adcode")
        if (feature.get("type") != "Feature" or not isinstance(adcode, str)
                or not re.fullmatch(r"[0-9]{6}", adcode)
                or props.get("gb") != "156" + adcode or props.get("level") != level
                or not isinstance(props.get("name"), str) or not props["name"].strip()
                or feature.get("geometry", {}).get("type") not in ("Polygon", "MultiPolygon")):
            raise BoundarySelectionError("boundary_source_invalid", "Boundary asset identity fields or geometry type are invalid")
        if adcode in seen:
            raise BoundarySelectionError("boundary_source_invalid", "Duplicate adcode in one administrative level: " + adcode)
        seen.add(adcode)
    return features


def _load_layer(level):
    relative = "data/china_" + level + ".geojson"
    manifest = _manifest()
    entries = [item for item in manifest["assets"] if item.get("local_path") == relative]
    if len(entries) != 1:
        raise BoundarySelectionError("boundary_source_invalid", "Boundary source manifest has no unique layer entry")
    source = entries[0]
    data = (PACKAGE_DIR / relative).read_bytes()
    if len(data) != source["bytes"] or hashlib.sha256(data).hexdigest() != source["sha256"]:
        raise BoundarySelectionError("boundary_source_invalid", "Packaged boundary asset differs from its verified upstream bytes: " + relative)
    features = _validate_layer(json.loads(data), level)
    if len(features) != source["feature_count"]:
        raise BoundarySelectionError("boundary_source_invalid", "Boundary feature count differs from the verified source")
    return features, source


def _query(name, adcode, level, contains=False):
    if level is not None and level not in LEVELS:
        raise BoundarySelectionError("invalid_arguments", "level must be province, city or county")
    if name is None and adcode is None:
        raise BoundarySelectionError("invalid_arguments", "Provide an administrative name or six-digit adcode")
    if name is not None and (not isinstance(name, str) or not name.strip()):
        raise BoundarySelectionError("invalid_arguments", "name must be a nonempty string")
    if adcode is not None and (not isinstance(adcode, str) or not re.fullmatch(r"[0-9]{6}", adcode)):
        raise BoundarySelectionError("invalid_arguments", "adcode must be an exact six-digit string")
    if not isinstance(contains, bool):
        raise BoundarySelectionError("invalid_arguments", "contains must be a boolean")
    name = name.strip() if name is not None else None
    matches = []
    for current in (level,) if level else LEVELS:
        features, source = _load_layer(current)
        for feature in features:
            props = feature["properties"]
            if adcode is not None and props["adcode"] != adcode:
                continue
            if name is not None and (name not in props["name"] if contains else name != props["name"]):
                continue
            matches.append((feature, source))
    return matches


def _identity(feature):
    return {key: feature["properties"][key] for key in ("name", "adcode", "gb", "level")}


def find_candidates(*, name=None, adcode=None, level=None, contains=False):
    """Return candidate identities only; partial/ambiguous names never select an ROI."""
    return [_identity(feature) for feature, _ in _query(name, adcode, level, contains)]


def _geometry_report(feature):
    try:
        from shapely.geometry import shape
        from shapely.validation import explain_validity
    except ImportError as exc:
        raise BoundarySelectionError("boundary_dependency_missing", "Boundary verification requires Shapely, already included with the configured GEE GeoPandas environment") from exc
    geometry = shape(feature["geometry"])
    bounds = list(geometry.bounds)
    if (geometry.geom_type not in ("Polygon", "MultiPolygon") or geometry.is_empty
            or not geometry.is_valid or len(bounds) != 4
            or not all(math.isfinite(value) for value in bounds)
            or bounds[0] < -180 or bounds[2] > 180 or bounds[1] < -90 or bounds[3] > 90):
        raise BoundarySelectionError("boundary_geometry_invalid", "Selected upstream boundary is invalid; no geometry was repaired: " + explain_validity(geometry))
    polygons = [geometry] if geometry.geom_type == "Polygon" else list(geometry.geoms)
    vertices = sum(len(polygon.exterior.coords) + sum(len(ring.coords) for ring in polygon.interiors) for polygon in polygons)
    return {"type": geometry.geom_type, "valid": True, "bounds": bounds,
            "polygon_parts": len(polygons), "vertices": vertices, "crs": "EPSG:4326",
            "axis_order": "longitude,latitude", "source_crs": CRS84}


def _publish_json(path, value):
    """Publish exclusively after the complete small JSON file has been written."""
    descriptor, temporary = tempfile.mkstemp(prefix=".boundary-", suffix=".tmp", dir=path.parent)
    try:
        with os.fdopen(descriptor, "w", encoding="utf-8") as stream:
            json.dump(value, stream, ensure_ascii=False, separators=(",", ":"), allow_nan=False)
            stream.write("\n")
            stream.flush()
        os.link(temporary, path)
    finally:
        Path(temporary).unlink(missing_ok=True)


def export_boundary(output_dir, *, adcode=None, name=None, level=None, expected_name=None, filename=None):
    """Write exactly one unchanged, valid polygon and provenance into output_dir.

    Select using an exact adcode whenever known. An exact name is accepted only
    if unique across the requested levels. Ambiguity returns candidates in the
    exception instead of choosing the first match or falling back to a rectangle.
    """
    matches = _query(name, adcode, level)
    candidates = [_identity(feature) for feature, _ in matches]
    if not matches:
        raise BoundarySelectionError("boundary_not_found", "No exact administrative boundary matches the requested identity; query candidates before changing the requested area")
    if len(matches) != 1:
        raise BoundarySelectionError("boundary_ambiguous", "Several administrative units match; select the intended adcode and level", candidates)
    feature, source = matches[0]
    selected = candidates[0]
    if expected_name is not None and (not isinstance(expected_name, str) or selected["name"] != expected_name.strip()):
        raise BoundarySelectionError("boundary_name_mismatch", "The selected adcode does not match the expected administrative name", candidates)
    geometry = _geometry_report(feature)
    if filename is None:
        filename = "boundary_{}_{}.geojson".format(selected["level"], selected["adcode"])
    if not isinstance(filename, str) or Path(filename).name != filename or not filename.endswith(".geojson"):
        raise BoundarySelectionError("invalid_arguments", "filename must be a simple .geojson filename inside output_dir")
    root = Path(output_dir).expanduser().resolve()
    root.mkdir(parents=True, exist_ok=True)
    boundary_path = root / filename
    provenance_path = root / (Path(filename).stem + ".provenance.json")
    if boundary_path.exists() or boundary_path.is_symlink() or provenance_path.exists() or provenance_path.is_symlink():
        raise BoundarySelectionError("output_exists", "Boundary output already exists; use its verified path or choose a new filename")
    document = {"type": "FeatureCollection", "crs": {"type": "name", "properties": {"name": CRS84}}, "features": [feature]}
    provenance = {"repository": _manifest()["repository"], "commit": SOURCE_COMMIT,
                  "skill": "china-admin-boundaries", "skill_version": "0.1.0",
                  "source": source, "source_statement": "GeoCode upstream declares Tianditu-sourced WGS84 administrative polygons; no independent legal or survey certification is asserted",
                  "selection": selected, "geometry": geometry,
                  "processing": "Exact single-feature extraction; original coordinates and attributes retained, no simplification, buffering, repair or datum relabeling",
                  "boundary_path": str(boundary_path)}
    # Publish provenance first: an interrupted write cannot expose a boundary
    # without its source record. Do not overwrite files created by another run.
    _publish_json(provenance_path, provenance)
    try:
        _publish_json(boundary_path, document)
    except BaseException:
        provenance_path.unlink(missing_ok=True)
        raise
    return {"status": "boundary_ready", "boundary_path": str(boundary_path),
            "provenance_path": str(provenance_path), "selected": selected,
            "geometry": geometry, "artifact_paths": [str(boundary_path), str(provenance_path)]}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--adcode")
    parser.add_argument("--name")
    parser.add_argument("--level", choices=LEVELS)
    parser.add_argument("--contains", action="store_true", help="Name substring search only; never used for export")
    parser.add_argument("--expected-name")
    parser.add_argument("--output-dir")
    parser.add_argument("--filename")
    args = parser.parse_args()
    try:
        if args.output_dir:
            if args.contains:
                raise BoundarySelectionError("invalid_arguments", "Use a candidate's exact adcode/name for export; substring queries do not export")
            result = export_boundary(args.output_dir, adcode=args.adcode, name=args.name, level=args.level, expected_name=args.expected_name, filename=args.filename)
        else:
            result = {"candidates": find_candidates(adcode=args.adcode, name=args.name, level=args.level, contains=args.contains)}
        print(json.dumps(result, ensure_ascii=False))
    except BoundarySelectionError as exc:
        print(json.dumps({"status": "failed", "error_code": exc.code, "message": str(exc), "candidates": exc.candidates}, ensure_ascii=False))
        raise SystemExit(2)


if __name__ == "__main__":
    main()
