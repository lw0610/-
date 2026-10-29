import json
import logging
import os
from typing import Any, Dict

import processing
from qgis.core import (
    QgsProcessingContext,
    QgsProcessingFeedback,
    QgsProject,
    QgsRasterLayer,
    QgsVectorLayer,
    QgsWkbTypes,
)

from autocarto.core.paths import get_project_root, resolve_project_path
from autocarto.utils.redaction import redact_sensitive_text
from autocarto.tools.system._shared.data_loading_support import (
    DataLoadingError, boolean_parameter, normalized_data_source, split_data_source,
)

from autocarto.tools.system._shared.resource_handle import (
    ResourceHandleError, require_handle, handle_entries, handle_entry_path, handle_summary,
)
from autocarto.tools.system._shared.canvas_refresh import refresh_canvas
from autocarto.tools.system._shared.input_path_resolver import resolve_input_path
from autocarto.tools.system._shared.output_manager import (
    resolve_output_path,
    sync_session_context_from_params,
)

logger = logging.getLogger(__name__)


def _crs_label(crs: Any) -> str:
    """Describe valid custom CRSs instead of reporting an empty auth id."""
    if crs is None or not crs.isValid():
        return "未定义或无效"
    return crs.authid() or crs.description() or "有效自定义 CRS"


def _format_input_resolution_error(raw_path: str, resolution: Dict[str, Any]) -> str:
    status = resolution.get("status")
    reason = resolution.get("reason")
    candidates = resolution.get("candidates") or []

    if status == "ambiguous":
        preview = ", ".join(candidates[:3])
        if len(candidates) > 3:
            preview = f"{preview}, ..."
        return f"错误：输入路径存在歧义 '{raw_path}'，候选: {preview}"

    if status == "invalid_path":
        return f"错误：输入路径无效 '{raw_path}' ({reason})"

    return f"错误：文件不存在 '{raw_path}'"


def _resolve_loading_source(raw_path, kind):
    filename, options = split_data_source(raw_path)
    absolute = str(resolve_project_path(filename))
    resolution = resolve_input_path(absolute, expected_kind=kind, search_dirs={"project_root": str(get_project_root())})
    if resolution.get("status") != "resolved":
        raise DataLoadingError("input_" + str(resolution.get("status") or "invalid"),
                               _format_input_resolution_error(raw_path, resolution), resolution)
    resolved = resolution["resolved_path"]
    return resolved + ("|" + "|".join(options) if options else ""), resolution


def _layer_data(layer, reused):
    geometry_type = QgsWkbTypes.geometryDisplayString(layer.geometryType()) if isinstance(layer, QgsVectorLayer) else "Raster"
    return {"layer_id": layer.id(), "layer_name": layer.name(), "source": redact_sensitive_text(layer.source()),
            "crs": _crs_label(layer.crs()), "geometry_type": geometry_type, "reused": reused,
            "shown": QgsProject.instance().layerTreeRoot().findLayer(layer.id()) is not None}


def _find_existing_source(source, kind):
    identity = normalized_data_source(source)
    expected_class = QgsVectorLayer if kind == "vector" else QgsRasterLayer
    for layer in QgsProject.instance().mapLayers().values():
        if not isinstance(layer, expected_class) or not layer.isValid():
            continue
        try:
            existing = normalized_data_source(layer.source())
            if existing == identity:
                return layer
            if kind == "vector" and existing[0] == identity[0] and (not existing[1] or not identity[1]):
                from qgis.core import QgsProviderRegistry
                sublayers = QgsProviderRegistry.instance().querySublayers(identity[0])
                if sublayers:
                    default_name = sublayers[0].name()
                    if (existing[1] or default_name) == (identity[1] or default_name):
                        return layer
        except (DataLoadingError, ValueError, OSError):
            continue
    return None


def _load_data_layer(params, kind):
    try:
        project = QgsProject.instance()
        show = boolean_parameter(params.get("show"), True)
        raw_path = params.get("path")
        layer_id = params.get("layer_id")
        if layer_id:
            layer = project.mapLayer(layer_id)
            expected = QgsVectorLayer if kind == "vector" else QgsRasterLayer
            if not isinstance(layer, expected) or not layer.isValid():
                raise DataLoadingError("input_missing", "已准备的图层不在当前工程中或类型不匹配: " + str(layer_id))
        elif raw_path:
            source, _ = _resolve_loading_source(raw_path, kind)
            layer = _find_existing_source(source, kind)
            if layer is not None:
                # A file may have been updated by an external processing script.
                layer.dataProvider().reloadData()
                if kind == "vector":
                    layer.updateFields()
                    layer.updateExtents(True)
                layer.triggerRepaint()
        else:
            raise DataLoadingError("invalid_arguments", "请提供文件路径 path 或已准备的 layer_id")
        reused = layer is not None
        if not reused:
            filename, _ = split_data_source(source)
            layer_name = params.get("layer_name") or os.path.splitext(os.path.basename(filename))[0]
            layer = QgsVectorLayer(source, layer_name, "ogr") if kind == "vector" else QgsRasterLayer(source, layer_name)
            if not layer.isValid():
                raise DataLoadingError("data_layer_invalid", "错误：无法加载图层，文件格式可能不支持或文件损坏")
            owner = params.get("_autocarto_output_owner") or {}
            if owner.get("session_id"):
                layer.setCustomProperty("autocarto/output_owner", json.dumps([owner["session_id"], owner.get("flow_run_id", ""), owner.get("step_id", "")]))
            project.addMapLayer(layer, False)
        if params.get("layer_name"):
            layer.setName(params["layer_name"])
        if show:
            root = project.layerTreeRoot()
            root.setHasCustomLayerOrder(False)
            node = root.findLayer(layer.id()) or root.insertLayer(0, layer)
            node.setItemVisibilityChecked(True)
            refresh_canvas()
        if kind == "vector":
            details = f"要素数: {layer.featureCount()}, 几何类型: {QgsWkbTypes.geometryDisplayString(layer.geometryType())}, 字段数: {len(layer.fields())}"
        else:
            details = f"宽度: {layer.width()}, 高度: {layer.height()}, 波段数: {layer.bandCount()}"
        return {"success": True, "output": layer.name(),
                "info": f"图层{'已显示' if show else '已准备，可用 layer_id 继续处理'}。{details}, 坐标系: {_crs_label(layer.crs())}",
                "data": _layer_data(layer, reused)}
    except DataLoadingError as exc:
        return {"success": False, "output": "", "info": str(exc), "message": str(exc),
                "error_type": exc.code, "data": exc.details}
    except Exception as exc:
        return {"success": False, "output": "", "info": f"加载图层失败: {exc}",
                "error_type": "data_load_failed", "message": str(exc)}


def load_vector_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    return _load_data_layer(params, "vector")


def export_raster_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:
        sync_session_context_from_params(params)
        layer_name = params.get("layer")
        output_path = params.get("output")

        if not layer_name:
            return {"success": False, "output": "", "info": "错误：缺少图层名称参数"}

        layers = QgsProject.instance().mapLayersByName(layer_name)
        if not layers:
            return {
                "success": False,
                "output": "",
                "info": f"错误：未找到图层 '{layer_name}'",
            }

        default_filename = f"{layer_name}_export.tif"
        output_path = resolve_output_path(output_path, default_filename, "exports")

        feedback = QgsProcessingFeedback()
        context = QgsProcessingContext()

        processing.run(
            "gdal:translate",
            {"INPUT": layers[0], "OUTPUT": output_path},
            context=context,
            feedback=feedback,
        )

        return {
            "success": True,
            "output": output_path,
            "info": f"栅格图层导出成功: {output_path}",
        }

    except Exception as e:
        return {"success": False, "output": "", "info": f"导出栅格图层失败: {str(e)}"}


def export_vector_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:
        from qgis.core import QgsVectorFileWriter

        session = sync_session_context_from_params(params)
        layer_name = params.get("layer")
        output_path = params.get("output")
        output_format = str(params.get("format") or "").strip()

        if not layer_name:
            return {"success": False, "output": "", "info": "错误：缺少图层名称参数"}

        exact = QgsProject.instance().mapLayer(str(layer_name))
        layers = [exact] if exact is not None else QgsProject.instance().mapLayersByName(str(layer_name))
        if not layers:
            return {
                "success": False,
                "output": "",
                "info": f"错误：未找到图层 '{layer_name}'",
            }
        if len(layers) != 1:
            raise DataLoadingError("ambiguous_layer", "同名图层不止一个，请提供明确的图层 ID",
                                   {"layer_ids": [layer.id() for layer in layers]})
        if not isinstance(layers[0], QgsVectorLayer) or not layers[0].isValid():
            raise DataLoadingError("invalid_vector_layer", "导出输入必须是有效的矢量图层")
        if params.get("replace_source") not in (None, ""):
            from autocarto.tools.system._shared.vector_source_replace import replace_vector_source
            return replace_vector_source(layers[0], params, session)

        ext_map = {
            "ESRI Shapefile": ".shp",
            "GeoJSON": ".geojson",
            "GPKG": ".gpkg",
            "GeoPackage": ".gpkg",
            "KML": ".kml",
        }
        if not output_format and output_path:
            lower_output = str(output_path).lower()
            if lower_output.endswith(".geojson") or lower_output.endswith(".json"):
                output_format = "GeoJSON"
            elif lower_output.endswith(".gpkg"):
                output_format = "GPKG"
            elif lower_output.endswith(".kml"):
                output_format = "KML"
            elif lower_output.endswith(".shp"):
                output_format = "ESRI Shapefile"
        if not output_format:
            output_format = "GeoJSON"
        ext = ext_map.get(output_format, ".shp")
        default_filename = f"{layers[0].name()}{ext}"
        output_path = resolve_output_path(output_path, default_filename, "exports")

        write_result = QgsVectorFileWriter.writeAsVectorFormat(
            layers[0],
            output_path,
            "UTF-8",
            layers[0].crs(),
            output_format,
        )
        if isinstance(write_result, tuple):
            error_code = write_result[0]
            error_message = write_result[1] if len(write_result) > 1 else ""
        else:
            error_code = write_result
            error_message = ""
        if error_code != QgsVectorFileWriter.NoError:
            return {
                "success": False,
                "output": "",
                "info": f"导出矢量图层失败: {error_message or error_code}",
            }

        result = {
            "success": True,
            "output": output_path,
            "info": f"矢量图层导出成功: {output_path}",
        }
        layer = layers[0]
        owner = json.loads(layer.customProperty("autocarto/output_owner", "[]"))
        session_id = str(params.get("session_id") or (params.get("_autocarto_output_owner") or {}).get("session_id") or getattr(session, "session_id", "") or "")
        if layer.customProperty("autocarto/field_working", False) or (layer.providerType() == "memory" and owner and owner[0] == session_id):
            from qgis.core import QgsMapLayerStyle
            from autocarto.tools.system._shared.vector_attributes_impl import _build_output_layer_ref
            style = QgsMapLayerStyle()
            style.readFromLayer(layer)
            layer.setDataSource(output_path, layer.name(), "ogr", False)
            style.writeToLayer(layer)
            layer.updateFields()
            layer.updateExtents()
            result["data"] = _layer_data(layer, True)
            result["output_layers"] = [_build_output_layer_ref(layer)]
            result["info"] += "；已保存当前工作图层，继续使用返回的 layer_id，无需再次加载"
            refresh_canvas()
        return result

    except DataLoadingError as e:
        return {"success": False, "output": "", "info": str(e), "error_type": e.code, "data": e.details}
    except Exception as e:
        return {"success": False, "output": "", "info": f"导出矢量图层失败: {str(e)}"}


def load_raster_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    return _load_data_layer(params, "raster")


def _place_layer_in_group(layer, name):
    root = QgsProject.instance().layerTreeRoot()
    group = root.findGroup(name) or root.addGroup(name)
    node = root.findLayer(layer.id())
    if node is not None and node.parent() != group:
        group.insertChildNode(0, node.clone())
        node.parent().removeChildNode(node)


def load_list_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    entries = []
    try:
        items = params.get("items")
        if not isinstance(items, list) or not items or len(items) > 10000:
            raise DataLoadingError("invalid_arguments", "items 必须是非空数据列表，每项提供 name 及 path 或 layer_id")
        rows = []
        seen = set()
        for item in items:
            if not isinstance(item, dict) or not isinstance(item.get("name"), str) or not item["name"].strip():
                raise DataLoadingError("invalid_arguments", "每项数据必须提供名称")
            if not item.get("layer_id") and (not isinstance(item.get("path"), str) or not item["path"].strip()):
                raise DataLoadingError("invalid_arguments", "每项数据必须提供文件路径 path 或已准备的 layer_id")
            if item["name"] in seen: raise DataLoadingError("invalid_arguments", "数据名称重复：" + item["name"])
            seen.add(item["name"])
            existing = QgsProject.instance().mapLayer(item["layer_id"]) if item.get("layer_id") else None
            filename, options = split_data_source(item["path"]) if item.get("path") else ("", [])
            path = resolve_project_path(filename) if filename else None
            kind = item.get("kind") or ("raster" if isinstance(existing, QgsRasterLayer) or (path and path.suffix.lower() in (".tif", ".tiff", ".img", ".vrt", ".asc", ".nc", ".jp2")) else "vector")
            if kind not in ("vector", "raster"): raise DataLoadingError("invalid_arguments", "kind 只能是 vector 或 raster")
            rows.append({"name": item["name"], "path": (str(path) + ("|" + "|".join(options) if options else "")) if path else "", "kind": kind, "required": boolean_parameter(item.get("required"), True), "exists": path is not None and path.is_file(), "layer_id": item.get("layer_id")})
        names = params.get("names")
        if names is not None and (not isinstance(names, list) or any(not isinstance(name, str) or not name.strip() for name in names)):
            raise DataLoadingError("invalid_arguments", "names 须为非空名称组成的数组")
        selected_names = {name.strip() for name in (names or [])}
        unknown = sorted(selected_names - {row["name"] for row in rows})
        if unknown:
            raise DataLoadingError("data_list_names_not_found", "数据列表没有指定名称", {"unmatched_names": unknown})
        strict = boolean_parameter(params.get("strict"), True)
        show = boolean_parameter(params.get("show"), True)
        group = params.get("group")
        if group is not None and not isinstance(group, str):
            raise DataLoadingError("invalid_arguments", "group 须为图层分组名称字符串")
        plans = []
        for row in rows:
            if selected_names and row["name"] not in selected_names:
                continue
            source = row["path"]
            entry = {"name": row["name"], "path": source, "status": "missing", "layer_id": "",
                     "crs": "", "geometry_type": "", "message": "", "required": row["required"]}
            try:
                layer_id = row.get("layer_id")
                if layer_id:
                    existing = QgsProject.instance().mapLayer(layer_id)
                    expected = QgsVectorLayer if row["kind"] == "vector" else QgsRasterLayer
                    if not isinstance(existing, expected) or not existing.isValid():
                        raise DataLoadingError("input_missing", "已准备的图层不在当前工程中，请重新准备数据: " + row["name"])
                    entry.update(status="failed", message="待复用")
                    plans.append((entry, source, row["kind"], existing))
                    entries.append(entry)
                    continue
                if not row["exists"]:
                    raise DataLoadingError("input_missing", "数据文件不存在: " + source)
                source, resolution = _resolve_loading_source(source, row["kind"])
                entry.update(path=source, status="failed", message="待加载")
                plans.append((entry, source, row["kind"], None))
            except DataLoadingError as exc:
                entry["message"] = str(exc)
            entries.append(entry)
        blocking = [entry for entry in entries if entry["status"] == "missing" and entry["required"]]
        if strict and blocking:
            for entry, _, _, _ in plans:
                entry["message"] = "严格检查未通过，未加载任何图层"
            return {"success": False, "output": "", "info": "必需数据缺失，未加载任何图层",
                    "error_type": "data_list_required_missing", "warnings": [], "data": {"entries": entries, "crs_summary": {}, "loaded_count": 0, "reused_count": 0}}
        summary = {}
        for entry, source, kind, existing in plans:
            result = _load_data_layer({"path": source, "layer_id": existing.id() if existing is not None else None,
                                       "layer_name": entry["name"], "show": show,
                                       "_autocarto_output_owner": params.get("_autocarto_output_owner")}, kind)
            if result.get("success"):
                data = result["data"]
                entry.update(status="reused" if data["reused"] else "loaded", layer_id=data["layer_id"],
                             crs=data["crs"], geometry_type=data["geometry_type"], message=result["info"],
                             layer_name=data["layer_name"])
                summary.setdefault(data["crs"], []).append({"layer_id": data["layer_id"], "layer_name": data["layer_name"]})
                if show and group and group.strip():
                    _place_layer_in_group(QgsProject.instance().mapLayer(data["layer_id"]), group.strip())
            else:
                entry.update(status="failed", message=result.get("info", "加载失败"))
        success = not any(entry["status"] == "failed" or (entry["status"] == "missing" and entry["required"]) for entry in entries)
        return {"success": success, "output": "", "info": "数据列表加载完成" if success else "数据列表加载完成，存在失败或必需数据缺失",
                "warnings": [],
                "data": {"entries": entries, "crs_summary": summary,
                         "loaded_count": sum(entry["status"] == "loaded" for entry in entries),
                         "reused_count": sum(entry["status"] == "reused" for entry in entries)}}
    except (DataLoadingError, ResourceHandleError) as exc:
        return {"success": False, "output": "", "info": str(exc), "message": str(exc),
                "error_type": exc.code, "data": dict(exc.details, entries=entries, crs_summary={})}
    except Exception as exc:
        return {"success": False, "output": "", "info": f"读取数据列表失败: {exc}",
                "error_type": "data_list_failed", "data": {"entries": entries, "crs_summary": {}}}
