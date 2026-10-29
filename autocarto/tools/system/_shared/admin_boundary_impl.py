"""从已校验的离线行政区资源精确提取原始面，不推断或修复边界。"""

import hashlib
import json
import math
import os
from pathlib import Path
import re
import tempfile

from autocarto.core.paths import (
    get_plugin_data_dir,
    prefer_project_relative_path,
    resolve_project_path,
)
from autocarto.tools.system._shared.tool_assets import tool_asset_path


class BoundarySelectionError(ValueError):
    def __init__(self, code, message, candidates=None):
        super().__init__(message)
        self.code = code
        self.candidates = candidates or []


def _normalize_query(params):
    levels = {"省": "province", "市": "city", "县": "county",
              "province": "province", "city": "city", "county": "county"}
    level = levels.get(str(params.get("level") or "").strip())
    if not level:
        raise BoundarySelectionError("invalid_arguments", "level 须为省、市、县或 province/city/county")
    name, code = params.get("name"), params.get("code")
    if name is not None:
        if not isinstance(name, str) or not name.strip():
            raise BoundarySelectionError("invalid_arguments", "name 须为非空行政区全名")
        name = name.strip()
    if code is not None and (not isinstance(code, str) or not re.fullmatch(r"[0-9]{6}", code)):
        raise BoundarySelectionError("invalid_arguments", "code 须为六位行政区代码字符串")
    if name is None and code is None:
        raise BoundarySelectionError("invalid_arguments", "必须提供 name 或 code")
    return level, name, code


def _boundary_asset(level):
    roots = (tool_asset_path("admin-boundaries", "中国行政区边界"),)
    filename = "china_" + level + ".geojson"
    for root in roots:
        for path in (root / filename, root / "data" / filename):
            if path.is_file():
                return root, path
    raise BoundarySelectionError("boundary_source_missing", "离线行政区边界数据不存在: " + filename)


def _load_boundary_features(level):
    root, path = _boundary_asset(level)
    try:
        manifest = json.loads((root / "upstream-manifest.json").read_text(encoding="utf-8"))
        if not isinstance(manifest.get("repository"), str) or not re.fullmatch(r"[0-9a-f]{40}", manifest.get("commit", "")):
            raise ValueError("来源仓库或提交信息无效")
        entries = [item for item in manifest["assets"] if item.get("local_path") == "data/" + path.name]
        if len(entries) != 1:
            raise ValueError("来源清单没有唯一的数据文件记录")
        source = entries[0]
        raw = path.read_bytes()
        if len(raw) != source["bytes"] or hashlib.sha256(raw).hexdigest() != source["sha256"]:
            raise ValueError("数据字节或哈希与来源清单不符")
        document = json.loads(raw)
        if document.get("crs") != {"type": "name", "properties": {"name": "urn:ogc:def:crs:OGC:1.3:CRS84"}}:
            raise ValueError("源坐标系不是原始 WGS84 经度/纬度 CRS84 声明")
        features = document.get("features")
        if document.get("type") != "FeatureCollection" or not isinstance(features, list) or not features or len(features) != source["feature_count"]:
            raise ValueError("要素集合或来源要素数无效")
        seen = set()
        for feature in features:
            props = feature.get("properties", {})
            code = props.get("adcode")
            if (feature.get("type") != "Feature" or not isinstance(code, str)
                    or not re.fullmatch(r"[0-9]{6}", code) or code in seen
                    or props.get("gb") != "156" + code or props.get("level") != level
                    or not isinstance(props.get("name"), str) or not props["name"].strip()
                    or feature.get("geometry", {}).get("type") not in {"Polygon", "MultiPolygon"}):
                raise ValueError("边界身份字段或面几何类型无效")
            seen.add(code)
        return features, {"repository": manifest["repository"], "commit": manifest["commit"],
                          "asset": source, "path": prefer_project_relative_path(str(path))}, path
    except (OSError, ValueError, KeyError, TypeError, AttributeError) as exc:
        raise BoundarySelectionError("boundary_source_invalid", "离线边界来源校验失败: " + str(exc)) from exc


def _select_boundary(params):
    level, name, code = _normalize_query(params)
    features, source, path = _load_boundary_features(level)
    matches = [feature for feature in features
               if (code is None or feature["properties"]["adcode"] == code)
               and (name is None or feature["properties"]["name"] == name)]
    candidates = [{key: feature["properties"][key] for key in ("name", "adcode", "gb", "level")}
                  for feature in matches]
    if not matches:
        raise BoundarySelectionError("boundary_not_found", "未找到精确匹配的行政区边界，请核对全名、代码和级别")
    if len(matches) != 1:
        raise BoundarySelectionError("boundary_ambiguous", "行政区名称重名，请根据候选指定六位代码", candidates)
    return matches[0], source, path


def _select_within(params):
    within = params.get("within")
    if not isinstance(within, str) or not within.strip():
        raise BoundarySelectionError("invalid_arguments", "within 须为全国、上级行政区全名或六位代码")
    if params.get("name") or params.get("code"):
        raise BoundarySelectionError("invalid_arguments", "within 与 name/code 不可混用")
    within = within.strip()
    level, _, _ = _normalize_query(dict(params, name=within))
    features, source, path = _load_boundary_features(level)
    if within in ("全国", "中国", "100000"):
        return features, source, path, {"name": "全国", "adcode": "100000", "level": level}
    candidates = []
    for parent_level in ("province", "city", "county"):
        parents, _, _ = _load_boundary_features(parent_level)
        candidates.extend(f for f in parents if f["properties"]["adcode"] == within or f["properties"]["name"] == within)
    # Municipalities and directly administered counties occur at two data levels.
    identities = {f["properties"]["adcode"]: f for f in candidates}
    if not identities:
        raise BoundarySelectionError("boundary_not_found", "未找到精确上级行政区")
    if len(identities) > 1:
        raise BoundarySelectionError("boundary_ambiguous", "上级行政区名称重名，请提供六位代码", [f["properties"] for f in identities.values()])
    parent = next(iter(identities.values()))["properties"]
    code = parent["adcode"]
    prefix = code[:2] if code.endswith("0000") else code[:4] if code.endswith("00") else code
    matches = [f for f in features if f["properties"]["adcode"].startswith(prefix)]
    if not matches:
        raise BoundarySelectionError("boundary_not_found", "此上级区域没有所选级别数据")
    return matches, source, path, dict(parent, level=level)


def _verify_geometry(path, code, layer=None, matches=None):
    from qgis.core import QgsFeatureRequest, QgsVectorLayer, QgsWkbTypes

    layer = layer or QgsVectorLayer(str(path), "boundary_source", "ogr")
    if not layer.isValid() or layer.crs().authid() != "EPSG:4326":
        raise BoundarySelectionError("boundary_source_invalid", "QGIS 无法读取原始 WGS84 边界")
    if matches is None:
        matches = list(layer.getFeatures(QgsFeatureRequest().setFilterExpression('"adcode" = \'' + code + "'")))
    if len(matches) != 1:
        raise BoundarySelectionError("boundary_source_invalid", "QGIS 读取的边界代码不唯一")
    geometry = matches[0].geometry()
    bounds = geometry.boundingBox()
    extent = [bounds.xMinimum(), bounds.yMinimum(), bounds.xMaximum(), bounds.yMaximum()]
    if (geometry.isNull() or geometry.isEmpty() or geometry.type() != QgsWkbTypes.PolygonGeometry
            or not geometry.isGeosValid() or not all(math.isfinite(value) for value in extent)
            or extent[0] < -180 or extent[2] > 180 or extent[1] < -90 or extent[3] > 90):
        raise BoundarySelectionError("boundary_geometry_invalid", "所选源面几何无效；未自动修复或替换边界")
    return {"valid": True, "type": QgsWkbTypes.displayString(geometry.wkbType()), "bounds": extent,
            "vertices": sum(1 for _ in geometry.vertices()), "crs": "EPSG:4326", "axis_order": "longitude,latitude"}


def _publish_exclusive(path, content):
    descriptor, temporary = tempfile.mkstemp(prefix=".boundary-", suffix=".tmp", dir=path.parent)
    try:
        with os.fdopen(descriptor, "wb") as stream:
            stream.write(content)
        os.link(temporary, path)
    finally:
        Path(temporary).unlink(missing_ok=True)


def extract_boundary_handler(params):
    try:
        if params.get("within") is not None:
            features, source, path, selected = _select_within(params)
            from qgis.core import QgsVectorLayer
            source_layer = QgsVectorLayer(str(path), "boundary_source", "ogr")
            # GeoJSON has no attribute index: one filtered scan per county is
            # quadratic. Index the selected codes in one read-only pass instead.
            by_code = {feature["properties"]["adcode"]: [] for feature in features}
            for item in source_layer.getFeatures():
                code = str(item["adcode"])
                if code in by_code:
                    by_code[code].append(item)
            checks = [_verify_geometry(path, code, source_layer, matches) for code, matches in by_code.items()]
            bounds = [min(v["bounds"][0] for v in checks), min(v["bounds"][1] for v in checks), max(v["bounds"][2] for v in checks), max(v["bounds"][3] for v in checks)]
            geometry = {"valid": True, "crs": "EPSG:4326", "bounds": bounds, "count": len(features), "vertices": sum(v["vertices"] for v in checks)}
        else:
            feature, source, path = _select_boundary(params)
            features = [feature]
            selected = {key: feature["properties"][key] for key in ("name", "adcode", "gb", "level")}
            geometry = _verify_geometry(path, selected["adcode"])
        raw_output = params.get("output")
        if raw_output is not None and (not isinstance(raw_output, str) or not raw_output.strip()):
            raise BoundarySelectionError("invalid_arguments", "output 须为 GeoJSON 文件路径")
        output = resolve_project_path(raw_output) if raw_output else get_plugin_data_dir() / "boundaries" / ("boundary_" + selected["level"] + "_" + selected["adcode"] + ".geojson")
        if output.suffix.lower() != ".geojson":
            raise BoundarySelectionError("invalid_arguments", "output 仅支持 .geojson 文件")
        provenance_path = output.with_suffix(".provenance.json")
        document = {"type": "FeatureCollection", "crs": {"type": "name", "properties": {"name": "urn:ogc:def:crs:OGC:1.3:CRS84"}}, "features": features}
        content = (json.dumps(document, ensure_ascii=False, separators=(",", ":"), allow_nan=False) + "\n").encode("utf-8")
        provenance = {"source": source, "selection": selected, "geometry": geometry,
                      "boundary_path": prefer_project_relative_path(str(output)),
                      "boundary_sha256": hashlib.sha256(content).hexdigest(),
                      "processing": "精确提取原始面，保留坐标与属性；未简化、缓冲、修复或改变基准"}
        provenance_content = (json.dumps(provenance, ensure_ascii=False, separators=(",", ":"), allow_nan=False) + "\n").encode("utf-8")
        reused_output = False
        if output.exists() or provenance_path.exists() or output.is_symlink() or provenance_path.is_symlink():
            if output.is_symlink() or provenance_path.is_symlink() or not output.is_file() or not provenance_path.is_file() or output.read_bytes() != content or provenance_path.read_bytes() != provenance_content:
                raise BoundarySelectionError("output_exists", "输出路径已存在且不属于同一份校验结果，未覆盖文件")
            reused_output = True
        else:
            output.parent.mkdir(parents=True, exist_ok=True)
            _publish_exclusive(provenance_path, provenance_content)
            try:
                _publish_exclusive(output, content)
            except BaseException:
                provenance_path.unlink(missing_ok=True)
                raise
        from autocarto.tools.system._shared.data_loading_impl import load_vector_handler

        level_name = {"province": "省级", "city": "市级", "county": "区县"}[selected["level"]] if params.get("within") is not None else ""
        layer_name = selected["name"] + level_name + "边界"
        loaded = load_vector_handler({"path": str(output), "layer_name": layer_name, "show": params.get("show"), "_autocarto_output_owner": params.get("_autocarto_output_owner")}) if params.get("load", True) else {"success": True, "data": {"layer_name": layer_name, "source": str(output)}}
        loaded["data"]["feature_count"] = len(features)
        if not loaded.get("success"):
            return loaded
        return {"success": True, "output": str(output), "info": "行政区原始边界已提取" + ("，已显示到地图" if loaded["data"].get("shown") else ""),
                "data": dict(loaded["data"], selected=selected, geometry=geometry, source_record=source,
                             provenance_path=str(provenance_path), reused_output=reused_output,
                             artifact_paths=[str(output), str(provenance_path)])}
    except BoundarySelectionError as exc:
        return {"success": False, "output": "", "info": str(exc), "message": str(exc),
                "error_type": exc.code, "data": {"candidates": exc.candidates}}
    except Exception as exc:
        return {"success": False, "output": "", "info": "提取行政区边界失败: " + str(exc),
                "error_type": "boundary_extract_failed", "message": str(exc)}
