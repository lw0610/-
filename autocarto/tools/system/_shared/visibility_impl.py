import importlib
import math
import os
from typing import Any, Dict, Iterable, Optional, Tuple

from autocarto.tools.system._shared.canvas_refresh import refresh_canvas
from autocarto.tools.system._shared.output_manager import resolve_output_path


def _parse_point(value: Any) -> Tuple[float, float]:
    if isinstance(value, (tuple, list)) and len(value) >= 2:
        return float(value[0]), float(value[1])

    if isinstance(value, dict):
        x = value.get("x", value.get("X"))
        y = value.get("y", value.get("Y"))
        if x is None or y is None:
            raise ValueError("点坐标字典缺少 x/y 字段")
        return float(x), float(y)

    if isinstance(value, str):
        raw = value.strip()
        if not raw:
            raise ValueError("点坐标为空")
        parts = [part.strip() for part in raw.split(",")]
        if len(parts) != 2:
            raise ValueError(f"点坐标格式错误: {value}")
        return float(parts[0]), float(parts[1])

    raise ValueError(f"不支持的点坐标类型: {type(value).__name__}")


def _is_memory_output_ref(value: Any) -> bool:
    if value is None:
        return True
    text = str(value).strip()
    if not text:
        return True
    return text.startswith("memory:") or text.upper() == "TEMPORARY_OUTPUT"


def _evaluate_line_of_sight(
    start_height: float,
    end_height: float,
    profile_samples: Iterable[Tuple[float, float]],
    epsilon: float = 1e-6,
) -> Tuple[bool, Optional[float]]:
    for fraction, terrain_height in profile_samples:
        los_height = start_height + (end_height - start_height) * fraction
        if terrain_height > los_height + epsilon:
            return False, fraction
    return True, None


def _resolve_raster_layer(layer_ref: Any):
    qgis_core = importlib.import_module("qgis.core")
    QgsProject = getattr(qgis_core, "QgsProject")
    QgsRasterLayer = getattr(qgis_core, "QgsRasterLayer")

    project = QgsProject.instance()
    candidate = str(layer_ref or "").strip()
    if not candidate:
        return None

    by_name = project.mapLayersByName(candidate)
    if by_name:
        layer = by_name[0]
        if isinstance(layer, QgsRasterLayer):
            return layer

    by_id = project.mapLayer(candidate)
    if by_id and isinstance(by_id, QgsRasterLayer):
        return by_id

    if os.path.exists(candidate):
        path_layer = QgsRasterLayer(candidate, os.path.basename(candidate))
        if path_layer.isValid():
            return path_layer

    return None


def line_of_sight_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:
        qgis_core = importlib.import_module("qgis.core")
        qtcore = importlib.import_module("qgis.PyQt.QtCore")
        processing = importlib.import_module("processing")
        QgsFeature = getattr(qgis_core, "QgsFeature")
        QgsField = getattr(qgis_core, "QgsField")
        QgsGeometry = getattr(qgis_core, "QgsGeometry")
        QgsPointXY = getattr(qgis_core, "QgsPointXY")
        QgsProject = getattr(qgis_core, "QgsProject")
        QgsVectorLayer = getattr(qgis_core, "QgsVectorLayer")
        QVariant = getattr(qtcore, "QVariant")
    except Exception as e:
        return {
            "success": False,
            "output": "",
            "info": f"line_of_sight 不可用: no_supported_backend ({e})",
        }

    try:
        raster_ref = params.get("INPUT")
        if not raster_ref:
            return {
                "success": False,
                "output": "",
                "info": "错误：缺少 INPUT(DEM) 参数",
            }

        start_xy = _parse_point(params.get("START_POINT"))
        end_xy = _parse_point(params.get("END_POINT"))

        raster_layer = _resolve_raster_layer(raster_ref)
        if raster_layer is None:
            return {
                "success": False,
                "output": "",
                "info": f"错误：未找到 DEM 图层 '{raster_ref}'",
            }

        band = int(params.get("BAND", 1))
        observer_height = float(params.get("OBSERVER_HEIGHT", 0.0))
        target_height = float(params.get("TARGET_HEIGHT", 0.0))

        start_point = QgsPointXY(start_xy[0], start_xy[1])
        end_point = QgsPointXY(end_xy[0], end_xy[1])
        distance = math.hypot(end_xy[0] - start_xy[0], end_xy[1] - start_xy[1])
        if distance <= 0:
            return {
                "success": False,
                "output": "",
                "info": "错误：START_POINT 与 END_POINT 不能相同",
            }

        provider = raster_layer.dataProvider()
        start_value, start_ok = provider.sample(start_point, band)
        end_value, end_ok = provider.sample(end_point, band)
        if not start_ok or not end_ok:
            return {
                "success": False,
                "output": "",
                "info": "错误：起点或终点不在 DEM 有效范围内",
            }

        start_height = float(start_value) + observer_height
        end_height = float(end_value) + target_height

        default_interval = max(
            abs(
                float(
                    getattr(raster_layer, "rasterUnitsPerPixelX", lambda: 0.0)() or 0.0
                )
            ),
            abs(
                float(
                    getattr(raster_layer, "rasterUnitsPerPixelY", lambda: 0.0)() or 0.0
                )
            ),
            1.0,
        )
        sample_interval = float(
            params.get("SAMPLE_INTERVAL", default_interval) or default_interval
        )
        if sample_interval <= 0:
            sample_interval = default_interval

        steps = max(2, int(distance / sample_interval) + 1)
        profile_samples = []
        for idx in range(1, steps - 1):
            fraction = idx / float(steps - 1)
            px = start_xy[0] + (end_xy[0] - start_xy[0]) * fraction
            py = start_xy[1] + (end_xy[1] - start_xy[1]) * fraction
            p = QgsPointXY(px, py)
            sample_value, sample_ok = provider.sample(p, band)
            if not sample_ok:
                continue
            profile_samples.append((fraction, float(sample_value)))

        visible, blocked_fraction = _evaluate_line_of_sight(
            start_height,
            end_height,
            profile_samples,
        )

        crs_authid = raster_layer.crs().authid() or "EPSG:4326"
        line_layer = QgsVectorLayer(
            f"LineString?crs={crs_authid}", "line_of_sight", "memory"
        )
        line_provider = line_layer.dataProvider()
        line_provider.addAttributes(
            [
                QgsField("visible", QVariant.Int),
                QgsField("distance", QVariant.Double),
                QgsField("start_z", QVariant.Double),
                QgsField("end_z", QVariant.Double),
                QgsField("blocked_f", QVariant.Double),
            ]
        )
        line_layer.updateFields()

        feat = QgsFeature(line_layer.fields())
        feat.setGeometry(QgsGeometry.fromPolylineXY([start_point, end_point]))
        feat.setAttributes(
            [
                1 if visible else 0,
                float(distance),
                float(start_height),
                float(end_height),
                float(blocked_fraction) if blocked_fraction is not None else None,
            ]
        )
        line_provider.addFeature(feat)
        line_layer.updateExtents()

        output_ref = params.get("OUTPUT")
        if _is_memory_output_ref(output_ref):
            output_name = str(params.get("OUTPUT_NAME") or "line_of_sight")
            line_layer.setName(output_name)
            QgsProject.instance().addMapLayer(line_layer)
            output_value = output_name
        else:
            resolved_path = resolve_output_path(
                str(output_ref),
                default_filename="line_of_sight.gpkg",
                subdir="temp",
            )
            if not os.path.splitext(resolved_path)[1]:
                resolved_path = f"{resolved_path}.gpkg"
            save_result = processing.run(
                "native:savefeatures",
                {
                    "INPUT": line_layer,
                    "OUTPUT": resolved_path,
                },
            )
            output_path = str(save_result.get("OUTPUT") or resolved_path)
            saved_layer = QgsVectorLayer(
                output_path,
                os.path.splitext(os.path.basename(output_path))[0],
                "ogr",
            )
            if saved_layer.isValid():
                QgsProject.instance().addMapLayer(saved_layer)
            output_value = output_path

        refresh_canvas()
        blocked_text = (
            f", 阻挡位置比例={blocked_fraction:.3f}"
            if blocked_fraction is not None
            else ""
        )
        return {
            "success": True,
            "output": output_value,
            "info": f"视线分析完成，可见={visible}{blocked_text}",
            "data": {
                "visible": visible,
                "distance": float(distance),
                "blocked_fraction": blocked_fraction,
                "sample_count": len(profile_samples),
            },
        }
    except Exception as e:
        return {
            "success": False,
            "output": "",
            "info": f"line_of_sight 执行失败: {e}",
        }
