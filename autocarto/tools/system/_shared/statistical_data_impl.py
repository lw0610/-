"""Read-only statistical joins and reproducible outputs; no QGIS dependency.

The QGIS tool wrapper deliberately does not load layers or change a project.
Every output belongs to a new run directory; source bytes are never rewritten.
"""
from __future__ import annotations

import copy
import csv
from datetime import datetime, timezone
import hashlib
import io
import json
import math
from pathlib import Path
import re
import shutil
import tempfile
from typing import Any, Mapping


DEFAULT_METADATA_FIELDS = {
    "indicator": "指标", "unit": "单位", "source": "数据来源", "database": "数据库",
}
DERIVED_FIELDS = {
    "stat_region", "stat_year", "stat_indicator", "stat_unit", "stat_source",
    "stat_database", "data_status",
}
MISSING_TOKENS = {"", "-", "--", "—", "–", "…", "...", "NA", "N/A", "NULL", "NONE"}
NUMBER = re.compile(r"[+-]?(?:\d+(?:\.\d*)?|\.\d+)(?:[eE][+-]?\d+)?\Z")
GROUPED_NUMBER = re.compile(r"[+-]?\d{1,3}(?:,\d{3})+(?:\.\d+)?\Z")


def _text(value: Any) -> str:
    return str(value).strip() if value is not None else ""


def _boolean(params: Mapping[str, Any], key: str) -> bool:
    value = params.get(key)
    if value is None or value == "":
        return False
    if not isinstance(value, bool):
        raise ValueError(f"{key} 必须为 boolean")
    return value


def _year(value: Any, label: str) -> int:
    if isinstance(value, bool) or not re.fullmatch(r"[0-9]{1,4}", _text(value)):
        raise ValueError(f"{label} 必须为 1—9999 的整数年份: {value!r}")
    result = int(value)
    if not 1 <= result <= 9999:
        raise ValueError(f"{label} 超出 1—9999")
    return result


def _number(raw: str, label: str) -> float | None:
    text = raw.strip()
    if text.upper() in MISSING_TOKENS:
        return None
    if GROUPED_NUMBER.fullmatch(text):
        text = text.replace(",", "")
    if not NUMBER.fullmatch(text):
        raise ValueError(f"{label} 含非法数值 {raw!r}；单位、百分号和说明文字不可混入数值")
    value = float(text)
    if not math.isfinite(value):
        raise ValueError(f"{label} 必须为有限数值: {raw!r}")
    return value


def _read_input(raw_path: Any, label: str) -> tuple[Path, bytes]:
    path = Path(_text(raw_path)).expanduser()
    if not _text(raw_path) or not path.is_absolute():
        raise ValueError(f"{label} 必须为绝对路径")
    path = path.resolve(strict=True)
    if not path.is_file():
        raise ValueError(f"{label} 不是文件: {path}")
    return path, path.read_bytes()


def _read_csv(content: bytes) -> tuple[list[str], list[tuple[int, dict[str, str]]]]:
    text = content.decode("utf-8-sig")
    reader = csv.reader(io.StringIO(text, newline=""), strict=True)
    try:
        headers = [value.strip() for value in next(reader)]
    except StopIteration:
        raise ValueError("CSV 为空") from None
    if not headers or any(not value for value in headers):
        raise ValueError("CSV 列名不得为空")
    if len(headers) != len(set(headers)):
        raise ValueError("CSV 存在重复列名（包含去除首尾空白后的重复）")
    rows = []
    for cells in reader:
        if not cells or all(not cell.strip() for cell in cells):
            continue
        if len(cells) != len(headers):
            raise ValueError(f"CSV 第 {reader.line_num} 行列数与表头不一致")
        rows.append((reader.line_num, dict(zip(headers, cells))))
    if not rows:
        raise ValueError("CSV 没有数据行")
    return headers, rows


def _metadata(params, headers, selected_rows):
    mapping = params.get("metadata_fields")
    if mapping is None:
        mapping = DEFAULT_METADATA_FIELDS
    if not isinstance(mapping, dict) or set(mapping) - set(DEFAULT_METADATA_FIELDS):
        raise ValueError("metadata_fields 只能包含 indicator/unit/source/database 的列名映射")
    mapping = {key: _text(value) for key, value in mapping.items()}
    if any(not value for value in mapping.values()) or len(set(mapping.values())) != len(mapping):
        raise ValueError("metadata_fields 的列名不得为空或重复")
    metadata, warnings = {}, []
    for key in DEFAULT_METADATA_FIELDS:
        column = mapping.get(key)
        values = {_text(row[column]) for _, row in selected_rows if column in headers}
        values.discard("")
        if len(values) > 1:
            raise ValueError(f"目标年份的 {key} 元数据不一致: {sorted(values)}")
        csv_value = next(iter(values), "")
        explicit = _text(params.get(key))
        if explicit and csv_value and explicit != csv_value:
            raise ValueError(f"显式 {key} 与 CSV 元数据冲突: {explicit!r} != {csv_value!r}")
        metadata[key] = explicit or csv_value or None
        if metadata[key] is None:
            warnings.append(f"未提供 {key} 元数据，保持未知，不推断")
    return metadata, set(mapping.values()), warnings


def _statistical_rows(params, csv_bytes, year):
    headers, rows = _read_csv(csv_bytes)
    year_field = _text(params.get("year_field")) or "年份"
    table_format = _text(params.get("table_format")) or "wide_year_rows"
    if table_format not in {"wide_year_rows", "long"}:
        raise ValueError("table_format 只支持 wide_year_rows 或 long")
    if year_field not in headers:
        raise ValueError(f"CSV 缺少年份列 {year_field!r}")
    region_field = _text(params.get("region_field")) or "地区"
    csv_value_field = _text(params.get("csv_value_field")) or "数值"
    if table_format == "long":
        if len({year_field, region_field, csv_value_field}) != 3:
            raise ValueError("long 格式的年份、地区、数值列必须互不相同")
        for field in (region_field, csv_value_field):
            if field not in headers:
                raise ValueError(f"CSV 缺少 long 格式列 {field!r}")
    selected, keys = [], set()
    for line_number, row in rows:
        row_year = _year(row[year_field], f"CSV 第 {line_number} 行年份")
        key = row_year
        if table_format == "long":
            name = _text(row[region_field])
            if not name:
                raise ValueError(f"CSV 第 {line_number} 行地区名为空")
            key = (row_year, name)
        if key in keys:
            raise ValueError(f"CSV 存在重复{'年份' if table_format == 'wide_year_rows' else '年份/地区键'}: {key}")
        keys.add(key)
        if row_year == year:
            selected.append((line_number, row))
    if not selected:
        raise ValueError(f"CSV 不包含目标年份 {year}")
    metadata, metadata_columns, warnings = _metadata(params, headers, selected)
    key_columns = {year_field} if table_format == "wide_year_rows" else {year_field, region_field, csv_value_field}
    if key_columns & metadata_columns:
        raise ValueError("metadata_fields 不得与年份、地区或数值列重叠")
    values = {}
    if table_format == "wide_year_rows":
        line_number, row = selected[0]
        region_columns = [field for field in headers if field not in metadata_columns | {year_field}]
        if not region_columns:
            raise ValueError("年度横表没有地区列")
        items = [(name, row[name], line_number) for name in region_columns]
    else:
        items = [(_text(row[region_field]), row[csv_value_field], line_number) for line_number, row in selected]
    for name, raw, line_number in items:
        values[name] = {"value": _number(raw, f"CSV 第 {line_number} 行地区 {name!r}"), "raw_value": raw}
    return values, metadata, warnings, table_format, len(rows)


def _validate_coordinates(geometry: dict, label: str):
    kind = geometry.get("type")
    coordinates = geometry.get("coordinates")
    def positions(line, minimum, ring=False):
        if not isinstance(line, list) or len(line) < minimum:
            raise ValueError(f"{label} 的 {kind} 坐标数量不足")
        for point in line:
            if (not isinstance(point, list) or len(point) < 2 or
                any(isinstance(v, bool) or not isinstance(v, (int, float)) or not math.isfinite(v) for v in point)):
                raise ValueError(f"{label} 含无效或非有限坐标")
        if ring and line[0] != line[-1]:
            raise ValueError(f"{label} 的面环没有闭合")
    def polygon(rings):
        if not isinstance(rings, list) or not rings:
            raise ValueError(f"{label} 的面坐标为空")
        for ring in rings:
            positions(ring, 4, True)
    if kind == "Polygon":
        polygon(coordinates)
    elif kind == "LineString":
        positions(coordinates, 2)
    elif kind in {"MultiPolygon", "MultiLineString"}:
        if not isinstance(coordinates, list) or not coordinates:
            raise ValueError(f"{label} 的坐标为空")
        for part in coordinates:
            polygon(part) if kind == "MultiPolygon" else positions(part, 2)
    else:
        raise ValueError(f"{label} 不支持几何类型 {kind!r}")


def _geography(content, name_field, value_field):
    def reject_constant(value):
        raise ValueError(f"GeoJSON 含非有限数值 {value}")

    document = json.loads(content.decode("utf-8-sig"), parse_constant=reject_constant)
    if not isinstance(document, dict) or document.get("type") != "FeatureCollection" or not isinstance(document.get("features"), list):
        raise ValueError("GeoJSON 必须为 FeatureCollection")
    polygons, boundaries, names = [], [], set()
    for index, feature in enumerate(document["features"]):
        label = f"GeoJSON 要素 {index}"
        if not isinstance(feature, dict) or feature.get("type") != "Feature":
            raise ValueError(f"{label} 不是 Feature")
        geometry = feature.get("geometry")
        if not isinstance(geometry, dict):
            raise ValueError(f"{label} 缺少几何")
        _validate_coordinates(geometry, label)
        if geometry["type"] in {"LineString", "MultiLineString"}:
            boundaries.append(copy.deepcopy(feature))
            continue
        properties = feature.get("properties")
        if not isinstance(properties, dict) or not isinstance(properties.get(name_field), str):
            raise ValueError(f"{label} 缺少字符串名称字段 {name_field!r}")
        name = properties[name_field].strip()
        if not name or name in names:
            raise ValueError(f"GeoJSON 面名称为空或重复: {name!r}；同名分片请先整理为 MultiPolygon")
        collision = (DERIVED_FIELDS | {value_field}) & set(properties)
        if collision:
            raise ValueError(f"派生字段与 GeoJSON 源属性冲突: {sorted(collision)}")
        names.add(name)
        polygons.append((name, copy.deepcopy(feature)))
    if not polygons:
        raise ValueError("GeoJSON 没有行政区面")
    # Preserve an explicit legacy CRS; omit collection bbox because each output
    # has a different feature set. Geometry and feature properties remain intact.
    envelope = {"type": "FeatureCollection"}
    if "crs" in document:
        envelope["crs"] = copy.deepcopy(document["crs"])
    return polygons, boundaries, envelope


def _input_record(path, content):
    return {"path": str(path), "sha256": hashlib.sha256(content).hexdigest(), "size_bytes": len(content)}


def _output_parent(params):
    raw_dir = _text(params.get("output_dir"))
    if raw_dir:
        parent = Path(raw_dir).expanduser()
        if not parent.is_absolute():
            raise ValueError("output_dir 必须为绝对路径")
    else:
        # The bridge injects this call's session context. Do not read the
        # process-global output_manager session, which may belong to a prior run.
        session_dir = _text(params.get("session_dir"))
        subdirs = params.get("session_subdirs") or {}
        intermediate = _text(subdirs.get("intermediate")) if isinstance(subdirs, Mapping) else ""
        if intermediate:
            parent = Path(intermediate).expanduser()
            if not parent.is_absolute():
                raise ValueError("session_subdirs.intermediate 必须为绝对路径")
            if session_dir and not parent.resolve().is_relative_to(Path(session_dir).expanduser().resolve()):
                raise ValueError("session_subdirs.intermediate 不在当前 session_dir 内")
            parent = parent / "statistical"
        elif session_dir:
            parent = Path(session_dir).expanduser()
            if not parent.is_absolute():
                raise ValueError("session_dir 必须为绝对路径")
            parent = parent / "intermediate" / "statistical"
        else:
            from autocarto.core.paths import get_autocarto_home
            parent = get_autocarto_home() / "data" / "statistical"
    return parent.resolve()


def _write_outputs(params, report, polygons, boundaries, envelope, long_rows, year, value_field):
    parent = _output_parent(params)
    parent.mkdir(parents=True, exist_ok=True)
    run_dir = Path(tempfile.mkdtemp(prefix=f"statistics-{year}-", dir=str(parent)))
    try:
        paths = {
            "polygons_geojson": str(run_dir / "statistical_polygons.geojson"),
            "boundaries_geojson": str(run_dir / "boundaries.geojson") if boundaries else None,
            "long_csv": str(run_dir / "statistical_long.csv"),
            "manifest": str(run_dir / "manifest.json"),
        }
        def write_json(path, data):
            with Path(path).open("x", encoding="utf-8") as handle:
                json.dump(data, handle, ensure_ascii=False, allow_nan=False, indent=2)
                handle.write("\n")
        write_json(paths["polygons_geojson"], {**envelope, "features": polygons})
        if boundaries:
            write_json(paths["boundaries_geojson"], {**envelope, "features": boundaries})
        with Path(paths["long_csv"]).open("x", encoding="utf-8-sig", newline="") as handle:
            writer = csv.DictWriter(handle, fieldnames=["stat_region", "stat_year", value_field, "raw_value", "data_status", "stat_indicator", "stat_unit", "stat_source", "stat_database"])
            writer.writeheader()
            writer.writerows(long_rows)
        report["paths"] = paths
        report["output_dir"] = str(run_dir)
        manifest = {"schema_version": "autocarto-statistical-data/v1", "created_at": datetime.now(timezone.utc).isoformat(), **report}
        write_json(paths["manifest"], manifest)
        return paths
    except Exception:
        # This run directory was exclusively created above; never touch inputs
        # or pre-existing output directories when a write fails.
        shutil.rmtree(run_dir)
        raise


def prepare_statistical_handler(params: Mapping[str, Any]) -> dict[str, Any]:
    """Prepare validated derivatives, or return a report without side effects."""
    try:
        validate_only = _boolean(params, "validate_only")
        allow_unmatched = _boolean(params, "allow_unmatched")
        year = _year(params.get("year"), "year")
        name_field = _text(params.get("name_field")) or "name"
        value_field = _text(params.get("value_field")) or "stat_value"
        if value_field in DERIVED_FIELDS or value_field == "raw_value":
            raise ValueError(f"value_field 与工具字段冲突: {value_field}")
        geojson_path, geojson_bytes = _read_input(params.get("geojson_path"), "geojson_path")
        csv_path, csv_bytes = _read_input(params.get("csv_path"), "csv_path")
        values, metadata, warnings, table_format, csv_row_count = _statistical_rows(params, csv_bytes, year)
        polygons, boundaries, envelope = _geography(geojson_bytes, name_field, value_field)
        names = {name for name, _ in polygons}
        excluded = params.get("exclude_names")
        if excluded is None:
            excluded = []
        if not isinstance(excluded, list) or any(not isinstance(name, str) or not name.strip() for name in excluded):
            raise ValueError("exclude_names 必须为非空地区名组成的数组")
        excluded = {name.strip() for name in excluded}
        unknown_exclusions = excluded - (names | set(values))
        if unknown_exclusions:
            raise ValueError(f"exclude_names 含不存在的地区: {sorted(unknown_exclusions)}")
        unmatched_csv = sorted(set(values) - names - excluded)
        unmatched_geojson = sorted(names - set(values) - excluded)
        missing = sorted(name for name in names & set(values) - excluded if values[name]["value"] is None)
        counts = {"polygon_count": len(polygons), "boundary_count": len(boundaries), "csv_row_count": csv_row_count, "csv_region_count": len(values), "valid_count": 0, "missing_count": len(missing), "unmatched_count": len(unmatched_geojson), "excluded_count": len(names & excluded)}
        long_rows, output_polygons, finite_values = [], [], []
        for name, feature in polygons:
            entry = values.get(name)
            value = entry["value"] if entry and name not in excluded else None
            status = "excluded" if name in excluded else "unmatched" if entry is None else "missing" if value is None else "valid"
            if status == "valid":
                finite_values.append(value)
                counts["valid_count"] += 1
            additions = {"stat_region": name, "stat_year": year, value_field: value, "data_status": status, **{f"stat_{key}": val for key, val in metadata.items()}}
            feature["properties"].update(additions)
            output_polygons.append(feature)
            long_rows.append({**additions, "raw_value": entry["raw_value"] if entry else ""})
        if missing:
            warnings.append(f"{len(missing)} 个地区数值缺失，保持 null；不等于零")
        if excluded:
            warnings.append(f"明确排除 {len(excluded)} 个地区，其面数值保持 null")
        if unmatched_csv or unmatched_geojson:
            warnings.append("存在未匹配地区；只去除首尾空白，未应用简称、模糊匹配或自动别名")
        if len(set(finite_values)) < 2:
            warnings.append("有效数值少于两个不同值，现有分级设色工具不能生成分级")
        report = {
            "validated": not (unmatched_csv or unmatched_geojson) or allow_unmatched,
            "validate_only": validate_only, "year": year, "table_format": table_format,
            "name_field": name_field, "value_field": value_field, "metadata": metadata,
            "counts": counts, "unmatched_csv_regions": unmatched_csv,
            "unmatched_geojson_regions": unmatched_geojson, "missing_regions": missing,
            "excluded_regions": sorted(excluded), "allow_unmatched": allow_unmatched,
            "value_range": {"min": min(finite_values), "max": max(finite_values)} if finite_values else None,
            "distinct_value_count": len(set(finite_values)), "warnings": warnings,
            "inputs": {"geojson": _input_record(geojson_path, geojson_bytes), "csv": _input_record(csv_path, csv_bytes)},
            "matching_policy": "exact_after_trim", "missing_tokens": sorted(MISSING_TOKENS),
            "session_id": _text(params.get("session_id")) or None,
            "project_path": _text(params.get("project_path")) or None,
            "paths": {},
        }
        if not report["validated"]:
            return {"success": False, "output": "", "info": "地区关联不完整；请核对未匹配清单，未生成文件", "data": report}
        if not validate_only:
            _write_outputs(params, report, output_polygons, boundaries, envelope, long_rows, year, value_field)
        # A .geojson in top-level output is automatically loaded by the tool
        # engine. Return the manifest and require explicit data_load_vector.
        return {"success": True, "output": report["paths"].get("manifest", ""), "info": f"{year} 年统计数据{'校验' if validate_only else '准备'}完成；有效 {counts['valid_count']}，缺失 {counts['missing_count']}，未匹配 {counts['unmatched_count']}，排除 {counts['excluded_count']}", "data": report}
    except (ValueError, TypeError, OSError, UnicodeError, csv.Error) as exc:
        return {"success": False, "output": "", "info": f"统计数据准备失败: {exc}", "data": {"validated": False, "paths": {}}}
