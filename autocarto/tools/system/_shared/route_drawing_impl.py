"""路线渲染、路线标签及旧表格组合入口。"""
import json
import hashlib
import math
from typing import (
    Any,
    Dict,
    List,
    Optional,
    Tuple,
)
from pathlib import Path
from qgis.PyQt.QtCore import QVariant
from qgis.PyQt.QtGui import (
    QColor,
    QFont,
)
from qgis.core import (
    Qgis,
    QgsCoordinateReferenceSystem,
    QgsCoordinateTransform,
    QgsFeature,
    QgsField,
    QgsGeometry,
    QgsUnitTypes,
    QgsLineSymbol,
    QgsPalLayerSettings,
    QgsProject,
    QgsProperty,
    QgsPointXY,
    QgsSingleSymbolRenderer,
    QgsTextBufferSettings,
    QgsTextFormat,
    QgsVectorLayerSimpleLabeling,
    QgsVectorLayer,
    QgsWkbTypes,
    QgsRuleBasedLabeling,
    QgsMapLayerDependency,
)
from autocarto.knowledge.dataset_handoff import (
    DatasetHandoffError,
    resolve_route_dataset,
)
from autocarto.tools.system._shared.table_io import (
    _get_col_value,
    _read_csv_rows,
    _to_bool,
)
from autocarto.tools.system._shared.route_points_impl import (
    _label_rendezvous_points,
    _prepare_route_points_layer,
)
from autocarto.tools.system._shared.style_common import _wrap_main_thread
from autocarto.knowledge.visual_presets import load_route_styles, renderer_defaults
from autocarto.tools.system._shared.resource_handle import handle_summary, require_handle
from autocarto.tools.system._shared.route_runtime_support import dataset_failure, renderer_directory, label_format
from autocarto.tools.system._shared.callouts_impl import cartography_add_callouts_handler
from autocarto.tools.system._shared.symbolize_mapping_impl import symbolize_points_by_mapping_handler


def draw_legacy_arrows_from_points_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    """
    调用 QGIS 插件绘制传统箭头（细箭头/燕尾攻击箭头/双箭头/三箭头）。

    参数说明（中文）：
    - plugin_dir: 插件目录（必填）
    - points: 控制点数组（必填），支持 [[x,y], ...] 或 [{x,y}, ...]
    - type: 图元类型（必填）
        * fine_arrow（细箭头，至少2点）
        * tailed_attack_arrow（燕尾攻击箭头，至少3点）
        * double_arrow（双箭头，至少3点）
        * three_arrow（三箭头，建议5-7点）
    - input_crs: 输入点坐标系（可选，默认 EPSG:4326）
    """
    try:
        import importlib
        import sys
        from pathlib import Path

        plugin_dir = params.get("plugin_dir")
        points = params.get("points")
        plot_type = str(params.get("type", "") or "").strip()
        input_crs_authid = str(params.get("input_crs", "EPSG:4326") or "EPSG:4326").strip()

        if not plugin_dir:
            return {"success": False, "output": "", "info": "错误：缺少 plugin_dir 参数"}
        if not isinstance(points, list):
            return {"success": False, "output": "", "info": "错误：points 必须为数组"}

        type_to_style = {
            "fine_arrow": "fineArrow",
            "tailed_attack_arrow": "tailedAttackArrow",
            "double_arrow": "doubleArrow",
            "three_arrow": "threeArrow",
        }
        min_points = {
            "fine_arrow": 2,
            "tailed_attack_arrow": 3,
            "double_arrow": 3,
            "three_arrow": 5,
        }
        if plot_type not in type_to_style:
            return {"success": False, "output": "", "info": f"错误：不支持的 type: {plot_type}"}
        if len(points) < min_points[plot_type]:
            return {"success": False, "output": "", "info": f"错误：{plot_type} 至少需要 {min_points[plot_type]} 个控制点"}

        plugin_path = Path(str(plugin_dir))
        service_path = plugin_path / "services" / "combined_plot_service.py"
        if not service_path.exists():
            return {"success": False, "output": "", "info": f"错误：未找到插件服务文件 {service_path}"}

        plugin_parent = str(plugin_path.parent)
        plugin_pkg = plugin_path.name
        module_name = f"{plugin_pkg}.services.combined_plot_service"
        if plugin_parent not in sys.path:
            sys.path.insert(0, plugin_parent)
        mod = importlib.import_module(module_name)

        from qgis.utils import iface
        if iface is None:
            return {"success": False, "output": "", "info": "错误：当前不在QGIS界面上下文（iface为空）"}

        project_crs = iface.mapCanvas().mapSettings().destinationCrs()
        src_crs = QgsCoordinateReferenceSystem(input_crs_authid)
        if not src_crs.isValid():
            return {"success": False, "output": "", "info": f"错误：input_crs 非法: {input_crs_authid}"}

        xform = None
        if src_crs.authid() != project_crs.authid():
            xform = QgsCoordinateTransform(src_crs, project_crs, QgsProject.instance())

        def _convert_point_like(p: Any):
            if isinstance(p, dict) and ("x" in p and "y" in p):
                qpt = QgsPointXY(float(p["x"]), float(p["y"]))
                if xform is not None:
                    qpt = xform.transform(qpt)
                return [qpt.x(), qpt.y()]
            if isinstance(p, (list, tuple)) and len(p) >= 2:
                qpt = QgsPointXY(float(p[0]), float(p[1]))
                if xform is not None:
                    qpt = xform.transform(qpt)
                return [qpt.x(), qpt.y()]
            raise ValueError("点坐标格式非法，需为 [x,y] 或 {x,y}")

        points_project = [_convert_point_like(p) for p in points]

        service = mod.CombinedPlotService(iface, str(plugin_path))
        result = service.add_plot_from_spec({
            "type": "legacy_arrow",
            "points": points_project,
            "style_id": type_to_style[plot_type],
            "options": {},
        })

        ok = bool((result or {}).get("ok"))
        return {
            "success": ok,
            "output": str((result or {}).get("feature_id", "")),
            "info": str((result or {}).get("message", "ok" if ok else "生成失败")),
            "data": result or {},
        }
    except Exception as e:
        return {"success": False, "output": "", "info": f"绘制传统箭头失败: {str(e)}"}



def draw_bezier_arrow_from_points_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    """
    调用 QGIS 插件绘制“经过点贝塞尔箭头”。

    参数说明（中文）：
    - plugin_dir: 插件目录（必填），例如 C:/.../RouteRendererPlugin
    - points: 经过点数组（必填），支持 [[x,y], ...] 或 [{x:..., y:...}, ...]，至少 2 个点
    - style_id: 样式ID（必填），例如 bezier_red_gradient / bezier_blue_gradient / route_red_reference
    - type: 图元类型（可选，默认 bezier_arrow），当前支持 bezier_arrow / branch_bezier / multi_start_bezier
    - input_crs: 输入点坐标系（可选，默认 EPSG:4326）
    - options: 扩展参数（可选）
        * width_scale: 样式宽度缩放比例（可选），例如 0.4
        * branch_bezier: 需提供 attach_point 与 branch_points
        * multi_start_bezier: 需提供 starts

    坐标处理逻辑（关键）：
    1) 先读取当前工程 CRS；
    2) 再读取输入点 CRS（默认 EPSG:4326）；
    3) 若两者不同，先转换到工程 CRS 再调用插件。
    """
    try:
        import importlib
        import sys
        from pathlib import Path

        plugin_dir = params.get("plugin_dir")
        points = params.get("points")
        style_id = str(params.get("style_id", "") or "").strip()
        plot_type = str(params.get("type", "bezier_arrow") or "bezier_arrow").strip()
        input_crs_authid = str(params.get("input_crs", "EPSG:4326") or "EPSG:4326").strip()
        options = params.get("options") or {}

        if not plugin_dir:
            return {"success": False, "output": "", "info": "错误：缺少 plugin_dir 参数"}
        if not isinstance(points, list) or len(points) < 2:
            return {"success": False, "output": "", "info": "错误：points 必须为至少2个点的数组"}
        if not style_id:
            return {"success": False, "output": "", "info": "错误：缺少 style_id 参数"}

        plugin_path = Path(str(plugin_dir))
        service_path = plugin_path / "services" / "combined_plot_service.py"
        if not service_path.exists():
            return {"success": False, "output": "", "info": f"错误：未找到插件服务文件 {service_path}"}

        plugin_parent = str(plugin_path.parent)
        plugin_pkg = plugin_path.name
        module_name = f"{plugin_pkg}.services.combined_plot_service"

        if plugin_parent not in sys.path:
            sys.path.insert(0, plugin_parent)

        # 强制从 plugin_dir 重新加载，避免 QGIS 会话内一直用旧版 combined_plot_service（改插件后“毫无变化”）
        for stale in list(sys.modules.keys()):
            if stale == module_name or stale.startswith(plugin_pkg + "."):
                del sys.modules[stale]
        mod = importlib.import_module(module_name)
        mod = importlib.reload(mod)

        try:
            _svc_mtime = int(service_path.stat().st_mtime)
        except Exception:
            _svc_mtime = None
        _fork_gap = hasattr(mod.CombinedPlotService, "_build_fork_gap_work") and hasattr(
            mod.CombinedPlotService, "_build_merge_controls_to_attach"
        )

        from qgis.utils import iface
        if iface is None:
            return {"success": False, "output": "", "info": "错误：当前不在QGIS界面上下文（iface为空）"}

        # 1) 读取工程 CRS
        project_crs = iface.mapCanvas().mapSettings().destinationCrs()
        # 2) 读取输入 CRS（默认 EPSG:4326）
        src_crs = QgsCoordinateReferenceSystem(input_crs_authid)
        if not src_crs.isValid():
            return {"success": False, "output": "", "info": f"错误：input_crs 非法: {input_crs_authid}"}

        # 3) CRS 不同则转换到工程 CRS
        xform = None
        if src_crs.authid() != project_crs.authid():
            xform = QgsCoordinateTransform(src_crs, project_crs, QgsProject.instance())

        def _convert_point_like(p: Any):
            if isinstance(p, dict) and ("x" in p and "y" in p):
                qpt = QgsPointXY(float(p["x"]), float(p["y"]))
                if xform is not None:
                    qpt = xform.transform(qpt)
                return [qpt.x(), qpt.y()]
            if isinstance(p, (list, tuple)) and len(p) >= 2:
                qpt = QgsPointXY(float(p[0]), float(p[1]))
                if xform is not None:
                    qpt = xform.transform(qpt)
                return [qpt.x(), qpt.y()]
            raise ValueError("点坐标格式非法，需为 [x,y] 或 {x,y}")

        points_project = [_convert_point_like(p) for p in points]

        options_project = options
        width_scale = None
        if isinstance(options, dict):
            options_project = dict(options)
            if "attach_point" in options_project and options_project.get("attach_point") is not None:
                options_project["attach_point"] = _convert_point_like(options_project.get("attach_point"))
            if "branch_points" in options_project and isinstance(options_project.get("branch_points"), list):
                options_project["branch_points"] = [_convert_point_like(p) for p in options_project.get("branch_points", [])]
            if "main_paths" in options_project and isinstance(options_project.get("main_paths"), list):
                main_paths_new = []
                for path_points in options_project.get("main_paths", []):
                    if isinstance(path_points, list):
                        main_paths_new.append([_convert_point_like(p) for p in path_points])
                options_project["main_paths"] = main_paths_new
            if "branches" in options_project and isinstance(options_project.get("branches"), list):
                branches_new = []
                for b in options_project.get("branches", []):
                    if not isinstance(b, dict):
                        continue
                    b2 = dict(b)
                    if "main_points" in b2 and isinstance(b2.get("main_points"), list):
                        b2["main_points"] = [_convert_point_like(p) for p in b2.get("main_points", [])]
                    if "attach_point" in b2 and b2.get("attach_point") is not None:
                        b2["attach_point"] = _convert_point_like(b2.get("attach_point"))
                    if "branch_points" in b2 and isinstance(b2.get("branch_points"), list):
                        b2["branch_points"] = [_convert_point_like(p) for p in b2.get("branch_points", [])]
                    branches_new.append(b2)
                options_project["branches"] = branches_new
            if "starts" in options_project and isinstance(options_project.get("starts"), list):
                starts_new = []
                for s in options_project.get("starts", []):
                    if not isinstance(s, dict):
                        continue
                    s2 = dict(s)
                    if "attach_point" in s2 and s2.get("attach_point") is not None:
                        s2["attach_point"] = _convert_point_like(s2.get("attach_point"))
                    if "start_points" in s2 and isinstance(s2.get("start_points"), list):
                        s2["start_points"] = [_convert_point_like(p) for p in s2.get("start_points", [])]
                    starts_new.append(s2)
                options_project["starts"] = starts_new
            if "width_scale" in options_project:
                try:
                    width_scale = float(options_project.get("width_scale"))
                except Exception:
                    return {"success": False, "output": "", "info": "错误：options.width_scale 必须为数字"}
                if width_scale <= 0:
                    return {"success": False, "output": "", "info": "错误：options.width_scale 必须大于 0"}

        # 可选：按比例缩放样式宽度参数（tail/neck/head/head_length）
        scaled_style_key = None
        original_style_snapshot = None
        if width_scale is not None and not getattr(mod.CombinedPlotService, "SUPPORTS_GROUP_WIDTH", False):
            scaled_style_key = style_id
            if plot_type in ("branch_bezier", "branch_network_bezier"):
                scaled_style_key = (getattr(mod, "BRANCH_BEZIER_SAME_STYLE_MAP", {}) or {}).get(style_id, style_id)
            elif plot_type == "multi_start_bezier":
                scaled_style_key = (getattr(mod, "MULTI_START_BEZIER_SAME_STYLE_MAP", {}) or {}).get(style_id, style_id)

            style_table = getattr(mod, "BEZIER_ARROW_STYLES", None)
            if not isinstance(style_table, dict) or scaled_style_key not in style_table:
                return {"success": False, "output": "", "info": f"错误：无法定位样式以应用 width_scale（style_id={style_id}）"}

            style_obj = style_table[scaled_style_key]
            original_style_snapshot = {
                "tail_width": float(style_obj.get("tail_width", 0.0)),
                "neck_width": float(style_obj.get("neck_width", 0.0)),
                "head_width": float(style_obj.get("head_width", 0.0)),
                "head_length": float(style_obj.get("head_length", 0.0)),
            }
            style_obj["tail_width"] = original_style_snapshot["tail_width"] * width_scale
            style_obj["neck_width"] = original_style_snapshot["neck_width"] * width_scale
            style_obj["head_width"] = original_style_snapshot["head_width"] * width_scale
            style_obj["head_length"] = original_style_snapshot["head_length"] * width_scale

        service = mod.CombinedPlotService(iface, str(plugin_path))
        try:
            spec = {
                "route_id": params.get("route_id", ""),
                "type": plot_type,
                "points": points_project,
                "style_id": style_id,
                "options": options_project,
            }
            if params.get("_prepare_only"):
                prepare = getattr(service, "prepare_plot_from_spec", None)
                result = prepare(spec) if prepare else {
                    "ok": True, "message": "当前插件保留原绘制入口",
                    "width_group_supported": False,
                }
            else:
                result = service.add_plot_from_spec(spec)
        finally:
            if scaled_style_key is not None and original_style_snapshot is not None:
                try:
                    style_obj = mod.BEZIER_ARROW_STYLES.get(scaled_style_key)
                    if isinstance(style_obj, dict):
                        style_obj.update(original_style_snapshot)
                except Exception:
                    pass

        ok = bool((result or {}).get("ok"))
        info = str((result or {}).get("message", "ok" if ok else "生成失败"))
        if xform is not None:
            info = f"{info}（已从 {src_crs.authid()} 转换到工程 {project_crs.authid()}）"
        # 便于确认是否加载到最新插件（fork_gap=true 表示分叉/汇入新逻辑已生效）
        ver_bits = []
        if _svc_mtime is not None:
            ver_bits.append(f"plugin_mtime={_svc_mtime}")
        ver_bits.append(f"fork_gap={'yes' if _fork_gap else 'NO-OLD-PLUGIN'}")
        ver_bits.append(f"plot={plot_type}")
        info = f"{info} [{' '.join(ver_bits)}]"

        return {
            "success": ok,
            "output": str((result or {}).get("feature_id", "")),
            "info": info,
            "data": {
                **(result or {}),
                "plugin_verify": {
                    "plugin_dir": str(plugin_path),
                    "service_mtime": _svc_mtime,
                    "fork_gap_logic": bool(_fork_gap),
                    "plot_type": plot_type,
                },
            },
        }
    except Exception as e:
        return {"success": False, "output": "", "info": f"绘制经过点贝塞尔箭头失败: {str(e)}"}



def _draw_bezier_route_batch(payloads: List[Dict[str, Any]], style_rows=None) -> List[Dict[str, Any]]:
    """Choose one safe width per semantic route group before writing features."""
    responses = [None] * len(payloads)
    groups = {}
    automatic_styles = {row.get("reference_style_id") or row["style_id"] for row in (style_rows or {}).values() if _to_bool(row.get("auto_width_group"))}
    for index, payload in enumerate(payloads):
        options = payload.get("options") or {}
        style = str(payload.get("style_id") or "")
        for prefix in ("branch_", "multi_start_"):
            if style.startswith(prefix):
                style = style[len(prefix):]
        explicit = options.get("width_group")
        # Compiled main paths retain their source route ID before this suffix.
        # Other styles retain their existing two-point symbol sizing.
        source = str(payload.get("route_id") or "").split("__render_", 1)[0]
        automatic = "red" in style if style_rows is None else style in automatic_styles
        if not explicit and (not automatic or not source):
            continue
        scale = float(options.get("width_scale", 1.0))
        key = (str(payload.get("plugin_dir") or ""), str(explicit or source), style, scale)
        groups.setdefault(key, []).append(index)

    for key, members in groups.items():
        explicit = any((payloads[i].get("options") or {}).get("width_group") for i in members)
        if len(members) < 2 and not explicit:
            continue
        factor = 1.0
        prepared = {}
        selected = {}
        supported = True
        for attempt in range(6):
            prepared, selected, reductions = {}, {}, []
            for index in members:
                payload = dict(payloads[index])
                options = dict(payload.get("options") or {})
                requested_scale = float(options.get("width_scale", 1.0))
                options.update(
                    width_scale=requested_scale * factor,
                    lock_body_width=True,
                    width_group={"id": key[1], "factor": factor,
                                 "requested_scale": requested_scale,
                                 "applied_scale": requested_scale * factor},
                )
                payload["options"] = options
                result = draw_bezier_arrow_from_points_handler(dict(payload, _prepare_only=True))
                data = result.get("data") or {}
                if data.get("width_group_supported") is False:
                    supported = False
                    break
                prepared[index], selected[index] = result, payload
                reduction = data.get("recommended_width_scale")
                if reduction is not None:
                    reduction = float(reduction)
                    if not math.isfinite(reduction) or not 0 < reduction < 1:
                        raise ValueError("插件返回的整组宽度缩放比例无效")
                    reductions.append(reduction)
            if not supported or not reductions:
                break
            if attempt < 5:
                # Leave a small numerical margin when rechecking the entire group.
                factor *= min(reductions) * 0.995
        if not supported:
            continue
        for index in members:
            result = prepared[index]
            if result.get("success"):
                responses[index] = draw_bezier_arrow_from_points_handler(selected[index])
            else:
                responses[index] = result
    for index, payload in enumerate(payloads):
        if responses[index] is None:
            responses[index] = draw_bezier_arrow_from_points_handler(payload)
    return responses



def _route_style_kind(draw_type: str, route_type: str = "") -> str:
    return renderer_defaults()["route_kind_by_type"].get(str(route_type).lower(),
            "branch" if draw_type == "branch_bezier" else "main")


def _route_curve_params(kind: str) -> Dict[str, float]:
    values = renderer_defaults()["curve_params"]
    return dict(values.get(kind, values["main"]))


def _route_reference_style_id(style_id: str, draw_type: str, style_rows=None, route_type: str = "") -> str:
    if style_rows is not None:
        for row in style_rows.values():
            if row["style_id"] == str(style_id).strip() and row["draw_type"] == str(draw_type).strip():
                return row.get("reference_style_id") or style_id
        return style_id
    style_id = str(style_id or "").strip()
    draw_type = str(draw_type or "").strip().lower()
    route_type = str(route_type or "").strip().lower()
    if route_type == "enemy" or draw_type == "multi_start_bezier":
        if style_id in ("bezier_blue_gradient", "multi_start_bezier_blue_gradient"):
            return "multi_start_route_blue_reference" if draw_type == "multi_start_bezier" else "route_blue_reference"
        if style_id in ("bezier_red_gradient", "multi_start_bezier_red_gradient"):
            return "multi_start_route_red_reference" if draw_type == "multi_start_bezier" else "route_red_reference"
    if route_type == "branch" or draw_type == "branch_bezier":
        if style_id == "branch_bezier_red_gradient":
            return "branch_route_red_reference"
        if style_id == "branch_bezier_blue_gradient":
            return "branch_route_blue_reference"
    if style_id == "bezier_red_gradient":
        return "route_red_reference"
    if style_id == "bezier_blue_gradient":
        return "route_blue_reference"
    return style_id


def _remove_repeated_control_coords(points: List[List[float]], eps: float = 1e-9) -> List[List[float]]:
    cleaned: List[List[float]] = []
    for point in points or []:
        if not point or len(point) < 2:
            continue
        coordinate = [float(point[0]), float(point[1])]
        if cleaned:
            dx = coordinate[0] - cleaned[-1][0]
            dy = coordinate[1] - cleaned[-1][1]
            if dx * dx + dy * dy <= eps * eps:
                continue
        cleaned.append(coordinate)
    return cleaned



def _build_handdrawn_route_controls(points: List[List[float]], route_id: str, kind: str = "main") -> List[List[float]]:
    """Return route control points unchanged; smoothing is handled in the plugin."""
    return [[float(p[0]), float(p[1])] for p in (points or []) if p and len(p) >= 2]



def _build_route_payloads_from_human_tables(dataset_dir: Path, plugin_dir: str, strict: bool = True, route_style=None, *, route_styles=None) -> List[Dict[str, Any]]:
    # 人类可维护路线数据的三张核心表：地点字典、路线配置、路径明细
    places_csv = dataset_dir / "places.csv"
    routes_csv = dataset_dir / "routes.csv"
    route_paths_csv = dataset_dir / "route_paths.csv"

    # 三张表缺一不可，先做文件存在性检查
    for p in [places_csv, routes_csv, route_paths_csv]:
        if not p.exists():
            raise ValueError(f"缺少文件: {p}")

    # 读取 CSV 原始行，后续按列名兼容不同写法
    places_rows = _read_csv_rows(places_csv)
    routes_rows = _read_csv_rows(routes_csv)
    path_rows = _read_csv_rows(route_paths_csv)

    # 第一步：建立“地名 -> 坐标”字典
    # 后面所有路线都不直接写经纬度，而是通过地名查坐标
    place_xy: Dict[str, Tuple[float, float]] = {}
    for r in places_rows:
        name = str(_get_col_value(r, ["name", "place_name"], required=True)).strip()
        if not name:
            continue
        # 严格模式下不允许重名地名，避免歧义
        if name in place_xy and strict:
            raise ValueError(f"places.csv 存在重名地点: {name}")
        x = float(_get_col_value(r, ["x", "lon", "lng"], required=True))
        y = float(_get_col_value(r, ["y", "lat"], required=True))
        place_xy[name] = (x, y)

    # 第二步：把 route_paths.csv 按 route_id 分组
    # 一个 route_id 对应一组主干/分叉/起点路径
    path_by_route: Dict[str, List[Dict[str, Any]]] = {}
    for r in path_rows:
        rid = str(_get_col_value(r, ["route_id"], required=True)).strip()
        path_by_route.setdefault(rid, []).append(r)

    use_builtin_style = route_style is None and route_styles is None
    style_rows, style_defaults = route_styles if route_styles is not None else load_route_styles(route_style)
    payloads: List[Dict[str, Any]] = []
    branch_network_groups: Dict[Tuple[str, str], Dict[str, Any]] = {}
    # 第三步：遍历 routes.csv 中启用的路线配置
    for rr in routes_rows:
        enabled = _to_bool(_get_col_value(rr, ["enabled"], default="1"))
        if not enabled:
            continue

        # 读取路线级配置
        rid = str(_get_col_value(rr, ["route_id"], required=True)).strip()
        notes = dict(part.split("=", 1) for part in str(rr.get("notes", "")).split(";") if "=" in part)
        topology = str(rr.get("topology") or notes.get("topology") or "standard")
        side = str(rr.get("side") or notes.get("side") or "neutral")
        preset = style_rows.get(style_defaults.get((topology, side)), {})
        draw_type = str(_get_col_value(rr, ["draw_type", "type"], default="")).strip() or preset.get("draw_type", "")
        if not draw_type:
            raise ValueError("路线缺少绘制方式: " + str(rid))
        route_type = str(_get_col_value(rr, ["route_type"], default="")).strip() or preset.get("route_type", "")
        style_id = str(_get_col_value(rr, ["style_id"], default="")).strip() or preset.get("style_id", "")
        if not style_id:
            raise ValueError("路线缺少样式编号: " + str(rid))
        style_id = _route_reference_style_id(style_id, draw_type, None if use_builtin_style else style_rows, route_type)
        input_crs = str(_get_col_value(rr, ["input_crs"], default="EPSG:4326")).strip() or "EPSG:4326"

        width_scale_raw = _get_col_value(rr, ["width_scale"], default="") or preset.get("width_scale", "")
        width_scale = None
        if width_scale_raw is not None and str(width_scale_raw).strip() != "":
            width_scale = float(width_scale_raw)

        # 从 route_paths.csv 中取出该 route 的所有路径行
        rows = path_by_route.get(rid, [])
        if not rows:
            if strict:
                raise ValueError(f"route_paths.csv 中未找到 route_id={rid}")
            continue

        # 工具函数：按 seq 排序并筛选某类路径行
        def _sorted_rows(filter_fn):
            tmp = [x for x in rows if filter_fn(x)]
            tmp.sort(key=lambda z: int(float(_get_col_value(z, ["seq"], default=0) or 0)))
            return tmp

        # 主干路径是所有路线的基础，至少要有 2 个点
        main_rows = _sorted_rows(lambda x: str(_get_col_value(x, ["path_type"], default="main")).strip() == "main")
        if len(main_rows) < 2:
            if strict:
                raise ValueError(f"route_id={rid} 的 main 段点数不足2")
            continue

        # 把主干上的地名转换成坐标序列
        main_points: List[List[float]] = []
        for mr in main_rows:
            pname = str(_get_col_value(mr, ["place_name", "name"], required=True)).strip()
            if pname not in place_xy:
                if strict:
                    raise ValueError(f"route_id={rid} main 地点不存在: {pname}")
                continue
            x, y = place_xy[pname]
            main_points.append([x, y])

        # 只把真实经过点交给插件。路线尽量保持原样，避免主线被揉成波浪。
        route_kind = _route_style_kind(draw_type, route_type)
        smooth_points = _remove_repeated_control_coords(main_points)

        # 统一构建 options，后面不同 draw_type 会往里补充参数
        options: Dict[str, Any] = {}
        # Text belongs to an individual path, including each incoming branch.
        # Old tables without label_text retain a route-name label on the main only.
        configured = next((row for row in style_rows.values() if (row.get("reference_style_id") or row["style_id"]) == style_id), preset)
        label_paths = (route_type == "enemy" or side == "enemy") if use_builtin_style else _to_bool(configured.get("label_paths", True))
        path_labels: List[Dict[str, Any]] = []

        def _path_label(path_rows, role, index=0):
            if not label_paths or not path_rows:
                return
            first = path_rows[0]
            value = str(first.get("label_text") or "").strip()
            if "label_text" not in first and role == "main":
                value = str(rr.get("route_name") or "").strip()
            if value:
                path_labels.append({"path_id": str(first.get("path_id") or role),
                                    "role": role, "index": index, "text": value})

        _path_label(main_rows, "main")
        if width_scale is not None:
            options["width_scale"] = width_scale
        width_group = str(_get_col_value(rr, ["width_group"], default="")).strip()
        if width_group:
            options["width_group"] = width_group
        target_scale_raw = _get_col_value(rr, ["target_scale_denominator", "target_scale", "scale_denominator"], default="")
        if str(target_scale_raw).strip() != "":
            options["target_scale_denominator"] = float(target_scale_raw)

        # 分叉贝塞尔：需要 attach_point + branch_points
        if draw_type == "branch_bezier":
            branch_rows = _sorted_rows(lambda x: str(_get_col_value(x, ["path_type"], default="")).strip() == "branch")
            if len(branch_rows) < 1:
                if strict:
                    raise ValueError(f"route_id={rid} 缺少 branch 段")
                continue
            attach_place = str(_get_col_value(branch_rows[0], ["attach_place"], required=True)).strip()
            if attach_place not in place_xy:
                if strict:
                    raise ValueError(f"route_id={rid} attach_place 不存在: {attach_place}")
                continue
            # 分叉点（从主干上分出去的锚点）
            options["attach_point"] = list(place_xy[attach_place])
            # 分叉路径点序列
            bp_raw: List[List[float]] = []
            for br in branch_rows:
                pname = str(_get_col_value(br, ["place_name", "name"], required=True)).strip()
                if pname not in place_xy:
                    if strict:
                        raise ValueError(f"route_id={rid} branch 地点不存在: {pname}")
                    continue
                bp_raw.append(list(place_xy[pname]))
            if len(bp_raw) < 1 and strict:
                raise ValueError(f"route_id={rid} branch_points 为空")
            options["branch_points"] = _remove_repeated_control_coords(bp_raw)
            _path_label(branch_rows, "branch")
        # 多起点贝塞尔：需要 starts=[{attach_point,start_points}, ...]
        elif draw_type == "multi_start_bezier":
            start_rows = [x for x in rows if str(_get_col_value(x, ["path_type"], default="")).strip() == "start"]
            start_groups: Dict[str, List[Dict[str, Any]]] = {}
            for sr in start_rows:
                pid = str(_get_col_value(sr, ["path_id"], default="start")).strip()
                start_groups.setdefault(pid, []).append(sr)
            starts: List[Dict[str, Any]] = []
            main_tangent_cache = _nearest_seg_tangent(smooth_points, smooth_points[len(smooth_points) // 2]) if len(smooth_points) >= 2 else [1.0, 0.0]
            for pid, grp in start_groups.items():
                grp.sort(key=lambda z: int(float(_get_col_value(z, ["seq"], default=0) or 0)))
                attach_place = str(_get_col_value(grp[0], ["attach_place"], required=True)).strip()
                if attach_place not in place_xy:
                    if strict:
                        raise ValueError(f"route_id={rid} start={pid} attach_place 不存在: {attach_place}")
                    continue
                attach_pt = list(place_xy[attach_place])
                sp_raw: List[List[float]] = []
                for g in grp:
                    pname = str(_get_col_value(g, ["place_name", "name"], required=True)).strip()
                    if pname not in place_xy:
                        if strict:
                            raise ValueError(f"route_id={rid} start={pid} 地点不存在: {pname}")
                        continue
                    sp_raw.append(list(place_xy[pname]))
                if len(sp_raw) < 1:
                    if strict:
                        raise ValueError(f"route_id={rid} start={pid} start_points 为空")
                    continue
                mt = _nearest_seg_tangent(smooth_points, attach_pt) if len(smooth_points) >= 2 else main_tangent_cache
                start_seed = sp_raw + [attach_pt]
                sp = start_seed[:-1]
                sp = _remove_repeated_control_coords(sp)
                starts.append({"attach_point": attach_pt, "start_points": sp})
                _path_label(grp, "start", len(starts) - 1)
            if len(starts) < 1 and strict:
                raise ValueError(f"route_id={rid} 缺少 starts")
            options["starts"] = starts

        if path_labels:
            options["path_labels"] = path_labels

        # 最终组装成底层绘制工具需要的 payload
        payloads.append({
            "route_id": rid,
            "plugin_dir": plugin_dir,
            "type": draw_type,
            "style_id": style_id,
            "points": smooth_points,
            "input_crs": input_crs,
            "options": options,
        })

    for (_style_id, _input_crs), group in branch_network_groups.items():
        main_paths: List[List[List[float]]] = []
        edge_map: Dict[Tuple[Tuple[float, float], Tuple[float, float]], Dict[str, Any]] = {}
        connect_specs = group.get("main_attach_nodes", []) or []
        node_sequences: List[List[List[float]]] = []
        for spec in connect_specs:
            if not isinstance(spec, dict):
                continue
            nodes = spec.get("nodes") or []
            attach = spec.get("attach_point")
            if not isinstance(nodes, list) or len(nodes) < 2 or not attach:
                continue
            attach_idx = None
            for idx, node in enumerate(nodes):
                if math.hypot(float(node[0]) - float(attach[0]), float(node[1]) - float(attach[1])) <= 1e-8:
                    attach_idx = idx
                    break
            if attach_idx is None:
                continue
            if attach_idx > 0:
                node_sequences.append(nodes[:attach_idx + 1])
        if not node_sequences:
            node_sequences = [nodes for nodes in (group.get("main_nodes", []) or []) if isinstance(nodes, list)]

        for nodes in node_sequences:
            if not isinstance(nodes, list) or len(nodes) < 2:
                continue
            for idx in range(len(nodes) - 1):
                a = nodes[idx]
                b = nodes[idx + 1]
                edge_key = tuple(sorted((
                    (round(float(a[0]), 6), round(float(a[1]), 6)),
                    (round(float(b[0]), 6), round(float(b[1]), 6)),
                )))
                item = edge_map.setdefault(edge_key, {
                    "a": list(a),
                    "b": list(b),
                    "count": 0,
                    "dist": math.hypot(float(b[0]) - float(a[0]), float(b[1]) - float(a[1])),
                })
                item["count"] += 1
        parent: Dict[Tuple[float, float], Tuple[float, float]] = {}

        def _find_node(node: Tuple[float, float]) -> Tuple[float, float]:
            parent.setdefault(node, node)
            while parent[node] != node:
                parent[node] = parent[parent[node]]
                node = parent[node]
            return node

        def _union_node(a_node: Tuple[float, float], b_node: Tuple[float, float]) -> bool:
            ra = _find_node(a_node)
            rb = _find_node(b_node)
            if ra == rb:
                return False
            parent[rb] = ra
            return True

        for edge_key, item in sorted(edge_map.items(), key=lambda kv: (-int(kv[1]["count"]), float(kv[1]["dist"]))):
            if not _union_node(edge_key[0], edge_key[1]):
                continue
            main_paths.append(_build_handdrawn_route_controls(
                [item["a"], item["b"]],
                f"branch-network:{edge_key}",
                "branch",
            ))
        if not main_paths:
            main_paths = [p for p in group.get("main_paths", []) if isinstance(p, list) and len(p) >= 2]
        branches = [b for b in group.get("branches", []) if isinstance(b, dict)]
        if not main_paths or not branches:
            continue
        width_scales = group.get("width_scales", []) or []
        width_scale = max(width_scales) * 0.88 if width_scales else None
        options: Dict[str, Any] = {
            "main_paths": main_paths,
            "branches": branches,
        }
        if width_scale is not None:
            options["width_scale"] = width_scale
        route_ids = group.get("route_ids", [])
        payloads.append({
            "route_id": "BRANCH_NETWORK_" + "_".join(route_ids),
            "plugin_dir": plugin_dir,
            "type": "branch_network_bezier",
            "style_id": group["style_id"],
            "points": main_paths[0],
            "input_crs": group["input_crs"],
            "options": options,
        })

    return payloads



def _stable_seed_from_text(text: str) -> int:
    s = str(text or "")
    h = hashlib.sha256(s.encode("utf-8")).hexdigest()
    return int(h[:16], 16)



def _nearest_seg_tangent(points: List[List[float]], anchor: List[float]) -> List[float]:
    """计算主线在锚点附近的切线方向（单位向量）。"""
    if len(points) < 2:
        return [1.0, 0.0]

    ax, ay = float(anchor[0]), float(anchor[1])
    best_i = 0
    best_d2 = float("inf")
    for i in range(len(points) - 1):
        x1, y1 = float(points[i][0]), float(points[i][1])
        x2, y2 = float(points[i + 1][0]), float(points[i + 1][1])
        vx, vy = x2 - x1, y2 - y1
        vv = vx * vx + vy * vy
        if vv <= 1e-12:
            continue
        t = ((ax - x1) * vx + (ay - y1) * vy) / vv
        t = max(0.0, min(1.0, t))
        px = x1 + vx * t
        py = y1 + vy * t
        d2 = (ax - px) * (ax - px) + (ay - py) * (ay - py)
        if d2 < best_d2:
            best_d2 = d2
            best_i = i

    tx = float(points[best_i + 1][0]) - float(points[best_i][0])
    ty = float(points[best_i + 1][1]) - float(points[best_i][1])
    n = math.hypot(tx, ty)
    if n <= 1e-12:
        return [1.0, 0.0]
    return [tx / n, ty / n]



def _label_route_paths(route_items, scope, label_layer_name="路线名称"):
    """Label the plugin's final curves, with source IDs retained for relayout."""
    project = QgsProject.instance()
    features, sources, warnings = [], {}, []
    for item in route_items:
        if not item.get("success"):
            continue
        source = project.mapLayer(item.get("layer_id", ""))
        if source is None:
            candidates = project.mapLayersByName(item.get("layer_name", ""))
            source = candidates[0] if candidates else None
        if source is None or source.fields().indexFromName("label_paths") < 0:
            continue  # Older drawing plugins have no final-curve label interface.
        feature = source.getFeature(int(item["feature_id"]))
        raw = feature["label_paths"] if feature.isValid() else None
        if not raw or str(raw) in ("NULL", "None"):
            continue
        for path_id, record in json.loads(str(raw)).items():
            if not record.get("text"):
                continue
            geometry = QgsGeometry.fromWkt(record.get("wkt") or "")
            if geometry.isNull() or geometry.isEmpty():
                warnings.append(f"{record['text']}：没有可独立标注的箭身，保留路线")
                continue
            features.append((source.id(), feature.id(), path_id, record, geometry))
            sources[source.id()] = source

    owned = [layer for layer in project.mapLayers().values()
             if layer.customProperty("autocarto/route_labels_scope", "") == scope]
    if not features:
        for layer in owned:
            if not layer.isEditable():
                project.removeMapLayer(layer.id())
        return {"count": 0, "warnings": warnings}
    layer = owned[0] if owned else QgsVectorLayer("LineString", label_layer_name, "memory")
    if layer.isEditable():
        return {"count": 0, "warnings": ["路线名称图层正在编辑，已保留当前标注"]}
    layer.setName(label_layer_name)
    if not owned:
        layer.setCustomProperty("autocarto/route_labels_scope", scope)
        layer.dataProvider().addAttributes([
            QgsField("source_layer", QVariant.String), QgsField("source_fid", QVariant.LongLong),
            QgsField("path_id", QVariant.String), QgsField("label_text", QVariant.String),
            QgsField("half_width_map", QVariant.Double),
            QgsField("label_length_m", QVariant.Double),
        ])
        layer.updateFields()
        project.addMapLayer(layer)
    layer.setCrs(next(iter(sources.values())).crs())
    provider = layer.dataProvider()
    if layer.fields().indexFromName("label_length_m") < 0:
        provider.addAttributes([QgsField("label_length_m", QVariant.Double)])
        layer.updateFields()
    provider.deleteFeatures([feature.id() for feature in layer.getFeatures()])
    rows = []
    for source_id, fid, path_id, record, geometry in features:
        row = QgsFeature(layer.fields())
        row.setGeometry(geometry)
        row.setAttributes([source_id, fid, path_id, record["text"], record.get("half_width_map", 0.),
                           record.get("label_length_m", 0.)])
        rows.append(row)
    ok, _ = provider.addFeatures(rows)
    if not ok:
        raise RuntimeError("路线名称图层写入失败")
    layer.setRenderer(QgsSingleSymbolRenderer(QgsLineSymbol.createSimple({"line_style": "no"})))
    # Keep real line geometry so map extent filtering and layout exports work.
    # The backup plugin updates these source-linked rows when it moves a route.
    settings = QgsPalLayerSettings()
    settings.fieldName = "label_text"
    settings.placement = Qgis.LabelPlacement.Curved
    settings.priority = 7
    settings.distUnits = QgsUnitTypes.RenderMapUnits
    properties = settings.dataDefinedProperties()
    properties.setProperty(QgsPalLayerSettings.LabelDistance,
                           QgsProperty.fromExpression('"half_width_map" * 1.3'))
    settings.setDataDefinedProperties(properties)
    line_settings = settings.lineSettings()
    line_settings.setPlacementFlags(Qgis.LabelLinePlacementFlag.AboveLine |
                                   Qgis.LabelLinePlacementFlag.BelowLine |
                                   Qgis.LabelLinePlacementFlag.MapOrientation)
    line_settings.setOverrunDistance(2.0)
    line_settings.setOverrunDistanceUnit(QgsUnitTypes.RenderMillimeters)
    settings.setLineSettings(line_settings)
    settings.setFormat(label_format("route"))
    # At small scales a long name cannot fit on a short curve. Place it beside
    # the same curve's midpoint instead of silently losing the name.
    label_size = renderer_defaults()["labels"]["route"]["size_mm"]
    short = (f'coalesce("label_length_m" * 1000 / nullif(@map_scale, 0) '
             f'< length("label_text") * {label_size}, false)')
    root = QgsRuleBasedLabeling.Rule(QgsPalLayerSettings())
    curved = QgsRuleBasedLabeling.Rule(settings)
    curved.setFilterExpression(f"NOT ({short})"); root.appendChild(curved)
    compact = QgsPalLayerSettings(settings)
    compact.geometryGeneratorEnabled = True
    compact.geometryGeneratorType = QgsWkbTypes.PointGeometry
    compact.geometryGenerator = 'line_interpolate_point($geometry, length($geometry) / 2)'
    compact.placement = Qgis.LabelPlacement.OrderedPositionsAroundPoint
    compact.wrapChar = "、"
    adjacent = QgsRuleBasedLabeling.Rule(compact)
    adjacent.setFilterExpression(short); root.appendChild(adjacent)
    layer.setLabeling(QgsRuleBasedLabeling(root)); layer.setLabelsEnabled(True)
    layer.setDependencies({QgsMapLayerDependency(source_id) for source_id in sources})
    layer.updateExtents(); layer.triggerRepaint()
    return {"count": len(rows), "layer_id": layer.id(), "warnings": warnings}



def route_draw_handler(params):
    """只绘制数据集路线与其标签，不创建专题点或版面元素。"""
    try:
        params = dict(params)
        route_style = require_handle(params["route_style"], "route_style") if params.get("route_style") else None
        resolved = resolve_route_dataset(params)
        plugin_dir = renderer_directory(params)
        route_styles = load_route_styles(route_style) if route_style is not None else None
        payloads = _build_route_payloads_from_human_tables(resolved.dataset_dir, plugin_dir,
                    strict=_to_bool(params.get("strict", True)), route_styles=route_styles)
        responses = _draw_bezier_route_batch(payloads, route_styles[0] if route_styles is not None else None)
        items = []
        for payload, response in zip(payloads, responses):
            data = response.get("data") or {}
            items.append({"route_id": payload.get("route_id"), "success": bool(response.get("success")),
                          "info": response.get("info", ""), "feature_id": response.get("output", ""),
                          "layer_name": data.get("layer_name", ""), "layer_id": data.get("layer_id", ""),
                          "payload": {"type": payload.get("type"), "style_id": payload.get("style_id")}})
        failed = sum(not item["success"] for item in items)
        routes = {"success": failed == 0, "count": len(items), "failed_count": failed, "items": items}
        labels = _label_route_paths(items, str(resolved.dataset_dir), str(params.get("label_layer_name") or "路线名称"))
        ids = list(dict.fromkeys([item["layer_id"] for item in items if item["layer_id"]] + ([labels["layer_id"]] if labels.get("layer_id") else [])))
        layers = [{"id": value, "name": QgsProject.instance().mapLayer(value).name()} for value in ids if QgsProject.instance().mapLayer(value)]
        return {"success": failed == 0, "output": "", "info": "路线绘制完成" if not failed else "部分路线绘制失败",
                "error_type": "route_draw_failed" if failed else None,
                **({"resource": handle_summary(route_style)} if route_style is not None else {}),
                "data": {"dataset": resolved.to_dict(), "routes": routes, "route_labels": labels, "layers": layers}}
    except DatasetHandoffError as exc:
        return dataset_failure(exc)
    except Exception as exc:
        return {"success": False, "output": "", "info": str(exc), "message": str(exc), "error_type": getattr(exc, "code", "route_draw_failed")}


draw_bezier_arrow_from_points_handler = _wrap_main_thread(draw_bezier_arrow_from_points_handler)
route_draw_handler = _wrap_main_thread(route_draw_handler)
