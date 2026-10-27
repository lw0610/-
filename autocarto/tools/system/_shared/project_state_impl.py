import logging
from autocarto.utils.redaction import redact_sensitive_text
import math
from pathlib import Path
from typing import Any, Dict, List, Optional

from qgis.PyQt.QtCore import QCoreApplication, QTimer
from qgis.core import (
    QgsMapLayer,
    QgsProject,
    QgsRasterLayer,
    QgsVectorLayer,
    QgsWkbTypes,
)
from qgis.utils import iface

from autocarto.core.tool_engine.project_metadata import resolve_project_identity
from autocarto.core.tool_engine.runtime import qgis_json_default

logger = logging.getLogger(__name__)

_DATA_FILE_SUFFIXES = {".json", ".geojson", ".gpkg", ".shp", ".csv", ".xlsx", ".xls"}


def _safe_finite(value: Any) -> Optional[float]:
    try:
        numeric = float(value)
    except (TypeError, ValueError):
        return None
    if math.isfinite(numeric):
        return numeric
    return None


def _json_safe_value(value: Any) -> Any:
    if value is None or isinstance(value, (bool, str, int)):
        return value
    if isinstance(value, float):
        return value if math.isfinite(value) else None
    if isinstance(value, dict):
        return {str(k): _json_safe_value(v) for k, v in value.items()}
    if isinstance(value, (list, tuple)):
        return [_json_safe_value(v) for v in value]

    converted = qgis_json_default(value)
    if isinstance(converted, dict):
        return {str(k): _json_safe_value(v) for k, v in converted.items()}
    if isinstance(converted, list):
        return [_json_safe_value(v) for v in converted]
    if isinstance(converted, float) and not math.isfinite(converted):
        return None
    return converted


def _preview_list(items: List[str], limit: int = 5) -> str:
    values = [str(item).strip() for item in items if str(item).strip()]
    if not values:
        return "无"
    if len(values) <= limit:
        return ", ".join(values)
    return f"{', '.join(values[:limit])} 等 {len(values)} 项"


def _summarize_project_output(info: Dict[str, Any]) -> str:
    display_name = str(
        info.get("display_name") or info.get("project_name") or "当前工程"
    )
    layer_count = info.get("layer_count") or {}
    data_files = info.get("data_files") or []
    file_names = []
    for item in data_files[:5]:
        file_path = str((item or {}).get("path") or "").strip()
        if file_path:
            file_names.append(Path(file_path).name)
    return (
        f"项目信息获取成功: {display_name}；"
        f"图层 {layer_count.get('total', 0)} 个（矢量 {layer_count.get('vector', 0)}，栅格 {layer_count.get('raster', 0)}）；"
        f"工程目录数据文件: {_preview_list(file_names)}"
    )


def _summarize_layer_info(layer_info: Dict[str, Any]) -> str:
    name = str(layer_info.get("name") or "未命名图层")
    layer_type = str(layer_info.get("type") or "unknown")
    raw_source = str(layer_info.get("source") or "").strip()
    source = Path(raw_source.split("|", 1)[0]).name if raw_source else "内存"
    if layer_type == "vector":
        fields = [
            str(field.get("name") or "") for field in (layer_info.get("fields") or [])
        ]
        feature_count = layer_info.get("feature_count", 0)
        geometry_type = str(layer_info.get("geometry_type") or "unknown")
        return f"{name}(vector/{geometry_type}, 要素 {feature_count}, 字段: {_preview_list(fields)}, 源: {source})"
    return f"{name}({layer_type}, 源: {source})"


def _summarize_selected_features_output(
    layer: QgsVectorLayer, features_info: List[Dict[str, Any]], selected_count: int
) -> str:
    field_names = [field.name() for field in layer.fields()]
    feature_preview: List[str] = []
    for feature in features_info[:3]:
        attrs = feature.get("attributes") or {}
        pairs = []
        for key in field_names[:4]:
            if key in attrs:
                pairs.append(f"{key}={attrs.get(key)}")
        if pairs:
            feature_preview.append("{" + ", ".join(pairs) + "}")
    return (
        f"图层 {layer.name()} 当前选中 {selected_count} 个要素；"
        f"字段: {_preview_list(field_names)}；"
        f"样例属性: {_preview_list(feature_preview, limit=3)}"
    )


def _collect_feature_info(
    layer: QgsVectorLayer, features: List[Any]
) -> List[Dict[str, Any]]:
    features_info = []
    field_names = [field.name() for field in layer.fields()]
    for feature in features:
        attr_values = [_json_safe_value(v) for v in feature.attributes()]
        features_info.append(
            {
                "id": feature.id(),
                "attributes": dict(zip(field_names, attr_values)),
                "geometry_type": QgsWkbTypes.displayString(
                    feature.geometry().wkbType()
                ),
            }
        )
    return features_info


def get_project_info(params: Optional[Dict[str, Any]] = None) -> Dict[str, Any]:
    try:
        project = QgsProject.instance()
        project_identity = resolve_project_identity(project.title(), project.fileName())
        crs = project.crs()
        layers = project.mapLayers()
        vector_count = sum(
            1 for layer in layers.values() if layer.type() == QgsMapLayer.VectorLayer
        )
        raster_count = sum(
            1 for layer in layers.values() if layer.type() == QgsMapLayer.RasterLayer
        )

        info = {
            "file_path": project_identity["file_path"] or "未保存",
            "title": project_identity["title"],
            "project_name": project_identity["project_name"],
            "display_name": project_identity["display_name"],
            "title_source": project_identity["title_source"],
            "crs": {
                "auth_id": crs.authid(),
                "description": crs.description(),
                "is_geographic": crs.isGeographic(),
                "units": _json_safe_value(crs.mapUnits()),
            },
            "layer_count": {
                "total": len(layers),
                "vector": vector_count,
                "raster": raster_count,
            },
            "is_dirty": project.isDirty(),
            "home_path": project.homePath(),
            "data_files": _collect_project_data_files(project.homePath()),
        }

        return {
            "success": True,
            "output": _summarize_project_output(info),
            "info": info,
        }

    except Exception as e:
        return {"success": False, "output": f"获取项目信息失败: {str(e)}", "info": {}}


def get_layers_info(params: Optional[Dict[str, Any]] = None) -> Dict[str, Any]:
    try:
        if params is None:
            params = {}

        include_invisible = params.get("include_invisible", True)
        project = QgsProject.instance()
        layers = project.mapLayers()

        layers_info = []
        for _, layer in layers.items():
            node = project.layerTreeRoot().findLayer(layer.id())
            visible = node is not None and node.isVisible()
            if not include_invisible and not visible:
                continue
            info = _get_layer_detailed_info(layer)
            info["visible"] = visible
            layers_info.append(info)

        layer_summaries = [
            _summarize_layer_info(layer_info) for layer_info in layers_info[:6]
        ]
        output_text = f"成功获取 {len(layers_info)} 个图层信息"
        if layer_summaries:
            output_text = f"{output_text}：{'; '.join(layer_summaries)}"

        return {
            "success": True,
            "output": output_text,
            "info": {"layers": layers_info, "count": len(layers_info)},
        }

    except Exception as e:
        return {"success": False, "output": f"获取图层信息失败: {str(e)}", "info": {}}


def get_active_layer_info(params: Optional[Dict[str, Any]] = None) -> Dict[str, Any]:
    try:
        if not iface:
            return {"success": False, "output": "无法访问QGIS界面", "info": {}}

        layer_name = str((params or {}).get("layer_name", "")).strip()
        if layer_name:
            layers = QgsProject.instance().mapLayersByName(layer_name)
            active_layer = layers[0] if layers else None
        else:
            active_layer = iface.activeLayer()
        if not active_layer:
            return {"success": False, "output": "当前没有选中的图层", "info": {}}

        layer_info = _get_layer_detailed_info(active_layer)

        return {
            "success": True,
            "output": f"活动图层: {_summarize_layer_info(layer_info)}",
            "info": layer_info,
        }

    except Exception as e:
        return {
            "success": False,
            "output": f"获取活动图层信息失败: {str(e)}",
            "info": {},
        }


def get_selected_features_info(
    params: Optional[Dict[str, Any]] = None,
) -> Dict[str, Any]:
    try:
        if params is None:
            params = {}

        layer_name = params.get("layer_name")
        max_features = params.get("max_features", 100)

        if layer_name:
            project = QgsProject.instance()
            layers = project.mapLayersByName(layer_name)
            if not layers:
                return {
                    "success": False,
                    "output": f"未找到图层: {layer_name}",
                    "info": {},
                }
            layer = layers[0]
        else:
            if not iface:
                return {"success": False, "output": "无法访问QGIS界面", "info": {}}
            layer = iface.activeLayer()

        if not layer:
            return {"success": False, "output": "没有可用的图层", "info": {}}

        if not isinstance(layer, QgsVectorLayer):
            return {
                "success": False,
                "output": "只能获取矢量图层的选中要素",
                "info": {},
            }

        selected_features = layer.selectedFeatures()
        if not selected_features:
            field_names = [field.name() for field in layer.fields()]
            preview_features = list(layer.getFeatures())[:max_features]
            preview_info = _collect_feature_info(layer, preview_features)
            return {
                "success": True,
                "output": _summarize_selected_features_output(layer, preview_info, 0),
                "info": {
                    "layer_name": layer.name(),
                    "selected_count": 0,
                    "fields": field_names,
                    "returned_count": len(preview_info),
                    "selection_fallback": "sample_features",
                    "features": preview_info,
                },
            }

        features_to_return = selected_features[:max_features]
        features_info = _collect_feature_info(layer, features_to_return)

        return {
            "success": True,
            "output": _summarize_selected_features_output(
                layer, features_info, len(selected_features)
            ),
            "info": {
                "layer_name": layer.name(),
                "selected_count": len(selected_features),
                "returned_count": len(features_info),
                "features": features_info,
            },
        }

    except Exception as e:
        return {
            "success": False,
            "output": f"获取选中要素信息失败: {str(e)}",
            "info": {},
        }


def open_project(params: Optional[Dict[str, Any]] = None) -> Dict[str, Any]:
    try:
        path = str((params or {}).get("path", "")).strip()
        if not path:
            return {"success": False, "output": "缺少 'path' 参数", "info": {}}

        project_path = Path(path).expanduser()
        if not project_path.exists():
            return {
                "success": False,
                "output": f"工程文件不存在: {project_path}",
                "info": {},
            }

        if not project_path.is_file():
            return {
                "success": False,
                "output": f"工程路径不是文件: {project_path}",
                "info": {},
            }

        project = QgsProject.instance()
        project.clear()
        success = project.read(str(project_path))
        if not success:
            logger.warning("读取 QGIS 工程失败: %s", project_path)
            return {
                "success": False,
                "output": f"读取工程失败: {project_path}",
                "info": {},
            }

        if iface and iface.mapCanvas():
            canvas = iface.mapCanvas()
            if hasattr(canvas, "refreshAllLayers"):
                canvas.refreshAllLayers()
            else:
                canvas.refresh()

        info = _build_project_summary(project, str(project_path))

        logger.info("成功打开 QGIS 工程: %s", project_path)
        return {
            "success": True,
            "output": f"成功打开工程: {project_path}",
            "info": info,
        }

    except Exception as e:
        logger.exception("打开 QGIS 工程失败")
        return {"success": False, "output": f"打开工程失败: {str(e)}", "info": {}}


def create_project(params: Optional[Dict[str, Any]] = None) -> Dict[str, Any]:
    try:
        path = str((params or {}).get("path", "")).strip()
        if not path:
            return {"success": False, "output": "缺少 'path' 参数", "info": {}}

        project_path = Path(path).expanduser()
        if project_path.suffix.lower() not in {".qgz", ".qgs"}:
            return {
                "success": False,
                "output": f"仅支持创建 .qgz / .qgs 工程: {project_path}",
                "info": {},
            }

        if project_path.exists():
            return {
                "success": False,
                "output": f"工程文件已存在: {project_path}",
                "info": {},
            }

        parent_dir = project_path.parent
        if not parent_dir.exists():
            return {
                "success": False,
                "output": f"工程目录不存在: {parent_dir}",
                "info": {},
            }

        if not parent_dir.is_dir():
            return {
                "success": False,
                "output": f"工程目录不可用: {parent_dir}",
                "info": {},
            }

        project = QgsProject.instance()
        project.clear()
        success = project.write(str(project_path))
        if not success:
            logger.warning("创建 QGIS 工程失败: %s", project_path)
            return {
                "success": False,
                "output": f"创建工程失败: {project_path}",
                "info": {},
            }

        if iface and iface.mapCanvas():
            canvas = iface.mapCanvas()
            if hasattr(canvas, "refreshAllLayers"):
                canvas.refreshAllLayers()
            else:
                canvas.refresh()

        info = _build_project_summary(project, str(project_path))

        logger.info("成功创建 QGIS 工程: %s", project_path)
        return {
            "success": True,
            "output": f"成功创建工程: {project_path}",
            "info": info,
        }

    except Exception as e:
        logger.exception("创建 QGIS 工程失败")
        return {"success": False, "output": f"创建工程失败: {str(e)}", "info": {}}


def close_instance(params: Optional[Dict[str, Any]] = None) -> Dict[str, Any]:
    try:
        params = params or {}
        delay_ms = params.get("delay_ms", 200)
        try:
            delay_ms = int(delay_ms)
        except (TypeError, ValueError):
            delay_ms = 200
        delay_ms = max(0, min(delay_ms, 5000))

        project = QgsProject.instance()
        project_path = project.fileName() or ""
        project_identity = resolve_project_identity(project.title(), project_path)
        is_dirty = bool(project.isDirty())

        exit_action = None
        if iface and hasattr(iface, "actionExit"):
            try:
                exit_action = iface.actionExit()
            except Exception:
                logger.exception("读取 QGIS 退出动作失败")

        main_window = None
        if iface and hasattr(iface, "mainWindow"):
            try:
                main_window = iface.mainWindow()
            except Exception:
                logger.exception("读取 QGIS 主窗口失败")

        app = QCoreApplication.instance()
        if exit_action is None and main_window is None and app is None:
            return {
                "success": False,
                "output": "当前实例缺少可用的退出能力",
                "info": {},
            }

        def _trigger_exit():
            try:
                if exit_action is not None:
                    logger.info("触发 QGIS 退出动作")
                    exit_action.trigger()
                    return
            except Exception:
                logger.exception("触发 QGIS 退出动作失败")

            try:
                if main_window is not None:
                    logger.info("回退为关闭 QGIS 主窗口")
                    main_window.close()
                    return
            except Exception:
                logger.exception("关闭 QGIS 主窗口失败")

            try:
                if app is not None:
                    logger.info("回退为调用 Qt 应用退出")
                    app.quit()
                    return
            except Exception:
                logger.exception("调用 Qt 应用退出失败")

            logger.warning("关闭实例请求未找到可执行退出路径")

        logger.info(
            "收到关闭实例请求: display_name=%s path=%s dirty=%s delay_ms=%d",
            project_identity["display_name"],
            project_identity["file_path"],
            is_dirty,
            delay_ms,
        )
        QTimer.singleShot(delay_ms, _trigger_exit)

        return {
            "success": True,
            "output": f"已发起关闭实例请求: {project_identity['display_name']}",
            "info": {
                "display_name": project_identity["display_name"],
                "project_path": project_identity["file_path"],
                "is_dirty": is_dirty,
                "delay_ms": delay_ms,
            },
        }

    except Exception as e:
        logger.exception("关闭实例失败")
        return {"success": False, "output": f"关闭实例失败: {str(e)}", "info": {}}


def _build_project_summary(project: QgsProject, project_path: str) -> Dict[str, Any]:
    project_identity = resolve_project_identity(
        project.title(), project.fileName() or project_path
    )
    layers = project.mapLayers()
    vector_count = sum(
        1 for layer in layers.values() if layer.type() == QgsMapLayer.VectorLayer
    )
    raster_count = sum(
        1 for layer in layers.values() if layer.type() == QgsMapLayer.RasterLayer
    )

    return {
        "file_path": project_identity["file_path"] or project_path,
        "title": project_identity["title"],
        "project_name": project_identity["project_name"],
        "display_name": project_identity["display_name"],
        "title_source": project_identity["title_source"],
        "home_path": project.homePath(),
        "layer_count": {
            "total": len(layers),
            "vector": vector_count,
            "raster": raster_count,
        },
        "is_dirty": project.isDirty(),
        "data_files": _collect_project_data_files(project.homePath()),
    }


def _collect_project_data_files(
    home_path: str, limit: int = 20
) -> List[Dict[str, Any]]:
    base_dir = Path(str(home_path or "").strip()).expanduser()
    if not base_dir.exists() or not base_dir.is_dir():
        return []

    files = []
    for path in sorted(base_dir.iterdir()):
        if not path.is_file():
            continue
        if path.suffix.lower() not in _DATA_FILE_SUFFIXES:
            continue
        files.append(
            {
                "name": path.name,
                "path": str(path),
                "suffix": path.suffix.lower(),
                "size_bytes": path.stat().st_size,
            }
        )
        if len(files) >= limit:
            break
    return files


def _get_layer_basic_info(layer: QgsMapLayer) -> Dict[str, Any]:
    crs = layer.crs()
    extent = layer.extent()

    basic_info = {
        "id": layer.id(),
        "name": layer.name(),
        "type": "vector" if layer.type() == QgsMapLayer.VectorLayer else "raster",
        "crs": {"auth_id": crs.authid(), "description": crs.description()},
        "is_valid": layer.isValid(),
        "extent": {
            "xmin": _safe_finite(extent.xMinimum()),
            "ymin": _safe_finite(extent.yMinimum()),
            "xmax": _safe_finite(extent.xMaximum()),
            "ymax": _safe_finite(extent.yMaximum()),
        },
    }

    if isinstance(layer, QgsVectorLayer):
        basic_info.update(
            {
                "geometry_type": QgsWkbTypes.displayString(layer.wkbType()),
                "feature_count": int(layer.featureCount()),
                "field_count": len(layer.fields()),
            }
        )
    elif isinstance(layer, QgsRasterLayer):
        basic_info.update(
            {
                "width": layer.width(),
                "height": layer.height(),
                "band_count": layer.bandCount(),
            }
        )

    return basic_info


def _get_layer_detailed_info(layer: QgsMapLayer) -> Dict[str, Any]:
    detailed_info = _get_layer_basic_info(layer)

    if isinstance(layer, QgsVectorLayer):
        fields_info = []
        for field in layer.fields():
            fields_info.append(
                {
                    "name": field.name(),
                    "type": field.typeName(),
                    "length": field.length(),
                    "precision": field.precision(),
                }
            )

        detailed_info.update(
            {
                "fields": fields_info,
                "selected_feature_count": layer.selectedFeatureCount(),
                "data_provider": layer.providerType(),
                "source": redact_sensitive_text(layer.source()),
            }
        )
    elif isinstance(layer, QgsRasterLayer):
        bands_info = []
        for i in range(1, layer.bandCount() + 1):
            bands_info.append({"band_number": i, "band_name": layer.bandName(i)})

        detailed_info.update(
            {
                "bands": bands_info,
                "data_provider": layer.providerType(),
                "source": redact_sensitive_text(layer.source()),
            }
        )

    return detailed_info
