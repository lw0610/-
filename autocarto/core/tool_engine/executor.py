import importlib
import importlib.util
import json
import logging
import os
import re
from typing import Any, Dict, List, Optional, Tuple

from .runtime import qgis_json_default

logger = logging.getLogger(__name__)

_ALGORITHM_PROVIDER_ORDER = (
    "native",
    "qgis",
    "gdal",
    "grass7",
    "grass",
    "saga",
    "pdal",
)

_ALGORITHM_ALIAS_OVERRIDES = {
    "buffer": "native:buffer",
    "convex_hull": "native:convexhull",
    "bounding_boxes": "native:boundingboxes",
    "minimum_bounding_geometry": "native:minimumboundinggeometry",
    "grass_v_buffer": "grass7:v.buffer",
}

_REPROJECTION_TOOLS = frozenset({"crs_reproject_vector", "reproject_layer"})


class ProcessingToolExecutor:
    def __init__(self, definitions: Dict[str, Dict[str, Any]]):
        self.definitions = definitions
        self._processing = None

    def _get_processing(self):
        if self._processing is None:
            try:
                self._processing = importlib.import_module("processing")
            except ImportError:
                logger.error("无法导入 processing 模块，请确保在 QGIS 环境中运行")
                raise
        return self._processing

    def execute(self, tool_name: str, params: Dict[str, Any]) -> Dict[str, Any]:
        definition = self.definitions.get(tool_name)
        if not definition:
            return {"success": False, "output": f"未知工具: {tool_name}", "info": ""}

        handler_path = definition.get("handler")
        handler_file = definition.get("handler_file")
        handler_function = definition.get("handler_function")
        if handler_file and handler_function:
            return self._execute_handler_file(
                str(handler_file), str(handler_function), params
            )
        if handler_path:
            return self._execute_handler(handler_path, params)

        algorithm_id = definition.get("algorithm_id")
        if not algorithm_id:
            return {
                "success": False,
                "output": f"工具 {tool_name} 缺少 algorithm_id 或 handler",
                "info": "",
            }

        return self._execute_algorithm(tool_name, definition, algorithm_id, params)

    def _execute_handler(
        self, handler_path: str, params: Dict[str, Any]
    ) -> Dict[str, Any]:
        try:
            module_path, func_name = handler_path.rsplit(".", 1)
            module = importlib.import_module(module_path)
            handler = getattr(module, func_name)
            return handler(params)
        except Exception as e:
            logger.error("执行 handler %s 失败: %s", handler_path, e)
            return {"success": False, "output": str(e), "info": "handler 执行失败"}

    def _execute_handler_file(
        self, handler_file: str, handler_function: str, params: Dict[str, Any]
    ) -> Dict[str, Any]:
        try:
            spec = importlib.util.spec_from_file_location(
                f"tool_handler_{abs(hash((handler_file, handler_function)))}",
                handler_file,
            )
            if spec is None or spec.loader is None:
                raise ImportError(f"无法加载 handler 文件: {handler_file}")
            module = importlib.util.module_from_spec(spec)
            spec.loader.exec_module(module)
            handler = getattr(module, handler_function)
            return handler(params)
        except Exception as e:
            logger.error(
                "执行文件 handler %s:%s 失败: %s",
                handler_file,
                handler_function,
                e,
            )
            return {"success": False, "output": str(e), "info": "handler 执行失败"}

    def _execute_algorithm(
        self,
        tool_name: str,
        definition: dict[str, Any],
        algorithm_id: str,
        params: dict[str, Any],
    ) -> Dict[str, Any]:
        retry_diagnostic = ""
        try:
            processing = self._get_processing()
            resolution = self.resolve_tool_algorithm(tool_name, definition)
            resolved_algorithm_id = resolution.get("resolved_id")

            if not resolution.get("available") or not resolved_algorithm_id:
                failure = self._build_unavailable_result(resolution)
                logger.error(
                    "算法不可用: tool=%s requested=%s resolved=%s reason=%s source=%s",
                    tool_name,
                    resolution.get("requested_id"),
                    resolution.get("resolved_id"),
                    resolution.get("reason"),
                    resolution.get("source"),
                )
                return failure

            logger.info(
                "开始执行工具: %s, 算法: %s (requested=%s, source=%s)",
                tool_name,
                resolved_algorithm_id,
                resolution.get("requested_id"),
                resolution.get("source"),
            )

            logger.debug("原始参数: %s", params)
            qgis_params = self._convert_params(definition, params)
            logger.info("执行算法 %s，参数: %s", resolved_algorithm_id, qgis_params)

            target_crs = self._validate_reprojection_request(tool_name, qgis_params)

            try:
                result = processing.run(resolved_algorithm_id, qgis_params)
            except Exception as first_error:
                retry_result, retry_diagnostic = self._retry_with_fixed_geometries_once(
                    processing=processing,
                    algorithm_id=resolved_algorithm_id,
                    definition=definition,
                    qgis_params=qgis_params,
                    original_error=first_error,
                )
                if retry_result is None:
                    raise
                result = retry_result

            validation_info = self._validate_reprojection_result(
                tool_name,
                result,
                target_crs,
                source_layer=qgis_params.get("INPUT"),
            )

            logger.info("算法执行成功，结果: %s", result)

            result_data, output_layers = self._publish_algorithm_outputs(
                definition, params, result
            )
            self._refresh_canvas()

            result_info = str(result_data)
            if validation_info:
                result_info = f"{result_info}; {validation_info}"
            if retry_diagnostic:
                result_info = f"{result_info}; {retry_diagnostic}"
            return {
                "success": True,
                "output": result_data,
                "info": result_info,
                "output_layers": output_layers,
            }
        except Exception as e:
            logger.error("执行工具 %s 失败: %s", tool_name, e, exc_info=True)
            failure_info = f"算法: {algorithm_id}, 参数: {params}"
            if retry_diagnostic:
                failure_info = f"{failure_info}; {retry_diagnostic}"
            return {
                "success": False,
                "output": f"执行失败: {str(e)}",
                "error": str(e),
                "info": failure_info,
            }

    def _validate_reprojection_request(
        self,
        tool_name: str,
        qgis_params: Dict[str, Any],
    ) -> Optional[Any]:
        """Resolve the requested target CRS before a reprojection is run.

        QGIS processing may accept an unresolved display name and still create a
        dataset whose geometry is valid but whose CRS is empty.  Treat that as a
        failed request instead of allowing the caller to load the output later.
        """

        if tool_name not in _REPROJECTION_TOOLS:
            return None

        target_value = qgis_params.get("TARGET_CRS")
        if target_value is None or str(target_value).strip() == "":
            raise ValueError("重投影失败：缺少目标 CRS")

        qgis_core = importlib.import_module("qgis.core")
        crs_class = getattr(qgis_core, "QgsCoordinateReferenceSystem")
        if isinstance(target_value, crs_class):
            target_crs = target_value
        else:
            target_crs = crs_class(str(target_value).strip())

        if target_crs is None or not target_crs.isValid():
            raise ValueError(
                "重投影失败：目标 CRS 无法解析；请使用有效的 EPSG、WKT 或 PROJ 定义，"
                "不要只传坐标系显示名称"
            )

        input_layer = qgis_params.get("INPUT")
        if hasattr(input_layer, "isValid") and hasattr(input_layer, "crs"):
            if not input_layer.isValid():
                raise ValueError("重投影失败：输入图层无效")
            input_crs = input_layer.crs()
            if input_crs is None or not input_crs.isValid():
                raise ValueError(
                    "重投影失败：输入图层没有有效 CRS；必须先确认原始坐标系，"
                    "不能直接猜测后转换"
                )
        return target_crs

    def _validate_reprojection_result(
        self,
        tool_name: str,
        result: Any,
        target_crs: Optional[Any],
        source_layer: Optional[Any] = None,
    ) -> str:
        """Reject unusable reprojection outputs before reporting success."""

        if tool_name not in _REPROJECTION_TOOLS:
            return ""
        if not isinstance(result, dict) or "OUTPUT" not in result:
            raise RuntimeError("重投影失败：处理结果未返回 OUTPUT")

        output = result.get("OUTPUT")
        if output is None:
            raise RuntimeError("重投影失败：OUTPUT 为空")

        qgis_core = importlib.import_module("qgis.core")
        if hasattr(output, "isValid") and hasattr(output, "crs"):
            layer = output
        elif isinstance(output, str) and output.strip():
            if output.strip().lower().startswith("memory:"):
                raise RuntimeError("重投影失败：未返回可验证的内存图层")
            vector_layer_class = getattr(qgis_core, "QgsVectorLayer")
            layer_name = os.path.splitext(os.path.basename(output))[0] or "reprojected"
            layer = vector_layer_class(output, layer_name, "ogr")
        else:
            raise RuntimeError("重投影失败：OUTPUT 类型无法验证")

        if not layer.isValid():
            raise RuntimeError("重投影失败：输出图层无效")

        output_crs = layer.crs()
        if output_crs is None or not output_crs.isValid():
            raise RuntimeError("重投影失败：输出图层没有有效 CRS，禁止继续加载")

        if target_crs is not None and not self._crs_equivalent(
            output_crs, target_crs
        ):
            raise RuntimeError("重投影失败：输出 CRS 与目标 CRS 不一致")

        if hasattr(source_layer, "featureCount") and hasattr(layer, "featureCount"):
            source_count = int(source_layer.featureCount())
            output_count = int(layer.featureCount())
            if source_count != output_count:
                raise RuntimeError(
                    "重投影失败：输出要素数与输入不一致 "
                    f"({output_count} != {source_count})"
                )

        auth_id = str(output_crs.authid() or "").strip()
        description = str(output_crs.description() or "").strip()
        crs_label = auth_id or description or "valid_custom_crs"
        return f"output_validation=passed; output_crs={crs_label}"

    def _crs_equivalent(self, left: Any, right: Any) -> bool:
        try:
            if left == right:
                return True
        except Exception:
            pass

        try:
            left_auth = str(left.authid() or "").strip().upper()
            right_auth = str(right.authid() or "").strip().upper()
            if left_auth and right_auth:
                return left_auth == right_auth
        except Exception:
            pass

        try:
            left_wkt = str(left.toWkt() or "").strip()
            right_wkt = str(right.toWkt() or "").strip()
            return bool(left_wkt and right_wkt and left_wkt == right_wkt)
        except Exception:
            return False

    def _retry_with_fixed_geometries_once(
        self,
        processing: Any,
        algorithm_id: str,
        definition: Dict[str, Any],
        qgis_params: Dict[str, Any],
        original_error: Exception,
    ) -> Tuple[Optional[Any], str]:
        error_text = str(original_error or "")
        if not self._is_geometry_error(error_text):
            return None, ""

        input_candidates = self._collect_geometry_retry_inputs(definition, qgis_params)
        if not input_candidates:
            diagnostic = (
                f"geometry_retry=skipped:no_layer_input; original_error={error_text}"
            )
            logger.info("几何重试跳过: %s", diagnostic)
            return None, diagnostic

        retry_params = dict(qgis_params)
        repaired_targets = []
        try:
            for param_name, param_value in input_candidates:
                if isinstance(param_value, list):
                    repaired_values = []
                    for idx, item in enumerate(param_value):
                        repaired_values.append(
                            self._fix_geometry_input(processing, item)
                        )
                        repaired_targets.append(f"{param_name}[{idx}]")
                    retry_params[param_name] = repaired_values
                else:
                    retry_params[param_name] = self._fix_geometry_input(
                        processing, param_value
                    )
                    repaired_targets.append(param_name)
        except Exception as fix_error:
            diagnostic = (
                "geometry_retry=failed_before_retry; "
                f"reason={fix_error}; original_error={error_text}"
            )
            logger.warning("几何修复失败，无法重试: %s", diagnostic)
            return None, diagnostic

        logger.info(
            "检测到几何错误，自动修复后重试: algorithm=%s targets=%s",
            algorithm_id,
            ",".join(repaired_targets),
        )
        try:
            retry_result = processing.run(algorithm_id, retry_params)
            diagnostic = (
                "geometry_retry=success; "
                f"targets={','.join(repaired_targets)}; original_error={error_text}"
            )
            logger.info("几何修复重试成功: %s", diagnostic)
            return retry_result, diagnostic
        except Exception as retry_error:
            diagnostic = (
                "geometry_retry=failed; "
                f"targets={','.join(repaired_targets)}; "
                f"original_error={error_text}; retry_error={retry_error}"
            )
            logger.error("几何修复后重试仍失败: %s", diagnostic)
            return None, diagnostic

    def _publish_algorithm_outputs(self, definition, params, result):
        """Keep counters/paths and register working vectors without displaying them."""
        data = dict(result)
        output_layers = []
        for parameter in definition.get("parameters", []):
            name, kind = parameter["name"], parameter.get("type")
            if name not in result:
                continue
            value = result[name]
            if kind == "output_layer":
                from qgis.core import QgsVectorLayer
                if definition.get("auto_load") is False:
                    # Diagnostics keep their counters; transient output layers
                    # are not map contents. Explicit file outputs stay available.
                    if isinstance(value, QgsVectorLayer) and value.providerType() == "memory":
                        data.pop(name)
                    else:
                        path = value.source() if isinstance(value, QgsVectorLayer) else str(value)
                        data[name] = path
                        output_layers.append({"path": path, "auto_load": False})
                    continue
                from autocarto.tools.system._shared.vector_attributes_impl import (
                    _build_output_layer_ref, _publish_target,
                )
                requested = str(params.get(name) or "")
                layer = value if isinstance(value, QgsVectorLayer) else QgsVectorLayer(str(value), name, "ogr")
                if not layer.isValid():
                    raise RuntimeError(f"算法输出图层无法读取：{name}={value}")
                if layer.providerType() == "memory":
                    target_name = requested.removeprefix("memory:") if requested.startswith("memory:") else ""
                else:
                    basename = os.path.splitext(os.path.basename(layer.source().split("|", 1)[0]))[0]
                    target_name = basename if re.search(r"[\u3400-\u9fff]", basename) else ""
                output_name = parameter.get("description") or name
                if output_name in {"输出图层", "输出"}:
                    output_name = definition.get("description") or output_name
                # Presentation is independent of the source used to replace an output.
                layer.setName(target_name or output_name)
                _publish_target(layer, params, output_ref=requested)
                reference = _build_output_layer_ref(layer)
                data[name] = reference
                output_layers.append(reference)
            elif kind == "file" and self._is_output_param(name, parameter) and isinstance(value, str):
                output_layers.append({"path": value, "auto_load": False})
        return json.loads(json.dumps(data, default=qgis_json_default)), output_layers

    def _is_geometry_error(self, error_message: str) -> bool:
        normalized = str(error_message or "").lower()
        if not normalized:
            return False

        keywords = (
            "invalid geometry",
            "无效几何",
            "could not write feature into output",
            "could not write feature",
        )
        return any(keyword in normalized for keyword in keywords)

    def _collect_geometry_retry_inputs(
        self,
        definition: Dict[str, Any],
        qgis_params: Dict[str, Any],
    ) -> list[tuple[str, Any]]:
        candidates = []
        for param_def in definition.get("parameters", []):
            name = str(param_def.get("name", "") or "")
            param_type = str(param_def.get("type", "") or "")
            if param_type not in ("layer", "multi_layer"):
                continue
            if name not in qgis_params:
                continue
            value = qgis_params.get(name)
            if value is None:
                continue
            candidates.append((name, value))
        return candidates

    def _fix_geometry_input(self, processing: Any, layer_input: Any) -> Any:
        fix_result = processing.run(
            "native:fixgeometries",
            {
                "INPUT": layer_input,
                "OUTPUT": "memory:",
            },
        )
        if not isinstance(fix_result, dict) or "OUTPUT" not in fix_result:
            raise RuntimeError("fix_geometries 未返回 OUTPUT")
        output = fix_result.get("OUTPUT")
        if output is None:
            raise RuntimeError("fix_geometries 返回空 OUTPUT")
        return output

    def _algorithm_exists(self, algorithm_id: str) -> bool:
        exists, _ = self._check_algorithm_availability(algorithm_id)
        return exists

    def _check_algorithm_availability(self, algorithm_id: str) -> Tuple[bool, str]:
        try:
            processing = self._get_processing()
            qgis_core = importlib.import_module("qgis.core")
            QgsApplication = getattr(qgis_core, "QgsApplication")

            registry = QgsApplication.processingRegistry()
            if not registry:
                logger.warning("Processing 注册表未初始化")
                return True, "registry_uninitialized"

            algorithm = registry.algorithmById(algorithm_id)
            if algorithm is None:
                logger.warning("算法 %s 在注册表中未找到", algorithm_id)
                try:
                    help_text = processing.algorithmHelp(algorithm_id)
                    if help_text:
                        logger.info("算法 %s 通过 algorithmHelp 找到", algorithm_id)
                        return True, "algorithm_help"
                except Exception:
                    pass
                if ":" not in str(algorithm_id):
                    return False, "invalid_algorithm_id"
                provider_id = str(algorithm_id).split(":", 1)[0]
                provider_exists = any(
                    provider.id() == provider_id for provider in registry.providers()
                )
                if not provider_exists:
                    return False, "provider_missing"
                return False, "invalid_algorithm_id"

            logger.debug("算法 %s 验证通过", algorithm_id)
            return True, "exact_match"
        except ImportError:
            return False, "processing_unavailable"
        except Exception as e:
            logger.error("检查算法 %s 时出错: %s", algorithm_id, e, exc_info=True)
            return True, "availability_check_error"

    def resolve_tool_algorithm(
        self, tool_name: str, definition: Dict[str, Any]
    ) -> Dict[str, Any]:
        algorithm_id = str(definition.get("algorithm_id", "") or "").strip()
        if not algorithm_id:
            return {
                "available": False,
                "tool": tool_name,
                "requested_id": "",
                "resolved_id": None,
                "source": "missing",
                "reason": "missing_algorithm_id",
            }

        resolution = self._resolve_algorithm_id(tool_name, algorithm_id)
        availability_note = str(definition.get("availability_note", "") or "").strip()
        if availability_note and not resolution.get("available"):
            resolution["detail_reason"] = availability_note
        return resolution

    def _resolve_algorithm_id(
        self, tool_name: str, requested_id: str
    ) -> Dict[str, Any]:
        available, reason = self._check_algorithm_availability(requested_id)
        if available:
            return {
                "available": True,
                "tool": tool_name,
                "requested_id": requested_id,
                "resolved_id": requested_id,
                "source": "exact",
                "reason": reason,
            }

        alias_resolution = self._resolve_algorithm_alias(requested_id)
        if alias_resolution is not None:
            return {
                "available": True,
                "tool": tool_name,
                "requested_id": requested_id,
                "resolved_id": alias_resolution["resolved_id"],
                "mapped_id": alias_resolution["resolved_id"],
                "source": "alias",
                "reason": alias_resolution["reason"],
            }

        return {
            "available": False,
            "tool": tool_name,
            "requested_id": requested_id,
            "resolved_id": None,
            "source": "exact",
            "reason": reason,
        }

    def _normalize_algorithm_token(self, text: str) -> str:
        return re.sub(r"[^a-z0-9]+", "", str(text or "").lower())

    def _build_algorithm_alias_candidates(self, requested_id: str) -> List[str]:
        raw = str(requested_id or "").strip()
        if not raw or ":" in raw:
            return []

        lowered = raw.lower()
        candidates: List[str] = []
        seen: set[str] = set()

        def _add(candidate_id: str) -> None:
            normalized = str(candidate_id or "").strip().lower()
            if not normalized or normalized in seen:
                return
            seen.add(normalized)
            candidates.append(normalized)

        override = _ALGORITHM_ALIAS_OVERRIDES.get(lowered)
        if override:
            _add(override)

        raw_variants: List[str] = []
        for variant in (
            lowered,
            lowered.replace("_", ""),
            lowered.replace("-", ""),
            lowered.replace("_", "."),
            lowered.replace("-", "."),
            self._normalize_algorithm_token(lowered),
        ):
            if variant and variant not in raw_variants:
                raw_variants.append(variant)

        provider_prefixed_variants: Dict[str, List[str]] = {}
        for provider in _ALGORITHM_PROVIDER_ORDER:
            prefix = f"{provider}_"
            if lowered.startswith(prefix):
                suffix = lowered[len(prefix) :].strip()
                if suffix:
                    provider_prefixed_variants[provider] = [
                        suffix,
                        suffix.replace("_", ""),
                        suffix.replace("_", "."),
                        self._normalize_algorithm_token(suffix),
                    ]
            if provider == "grass7" and lowered.startswith("grass_"):
                suffix = lowered[len("grass_") :].strip()
                if suffix:
                    provider_prefixed_variants[provider] = [
                        suffix,
                        suffix.replace("_", ""),
                        suffix.replace("_", "."),
                        self._normalize_algorithm_token(suffix),
                    ]

        for provider in _ALGORITHM_PROVIDER_ORDER:
            for variant in provider_prefixed_variants.get(provider, []):
                _add(f"{provider}:{variant}")

        for provider in _ALGORITHM_PROVIDER_ORDER:
            for variant in raw_variants:
                _add(f"{provider}:{variant}")

        return candidates

    def _resolve_algorithm_alias(
        self, requested_id: str
    ) -> Optional[Dict[str, str]]:
        for candidate_id in self._build_algorithm_alias_candidates(requested_id):
            available, reason = self._check_algorithm_availability(candidate_id)
            if available:
                logger.info(
                    "算法别名匹配成功: requested=%s resolved=%s reason=%s",
                    requested_id,
                    candidate_id,
                    reason,
                )
                return {"resolved_id": candidate_id, "reason": reason}
        return None

    def _build_unavailable_result(self, resolution: Dict[str, Any]) -> Dict[str, Any]:
        requested_id = resolution.get("requested_id") or "unknown"
        reason = resolution.get("reason") or "invalid_algorithm_id"
        source = resolution.get("source") or "exact"
        mapped_id = resolution.get("mapped_id")
        detail_reason = resolution.get("detail_reason") or ""

        if reason == "provider_missing":
            provider_id = (
                str(requested_id).split(":", 1)[0]
                if ":" in str(requested_id)
                else "unknown"
            )
            output = f"算法 {requested_id} 不可用：缺少 provider '{provider_id}'"
        elif reason == "missing_algorithm_id":
            output = "工具缺少 algorithm_id 配置"
        elif reason == "processing_unavailable":
            output = "Processing 环境不可用，无法执行算法工具"
        else:
            output = f"算法 {requested_id} 不存在或不可用"

        info_parts = [
            f"status={reason}",
            f"source={source}",
            f"requested={requested_id}",
        ]
        if mapped_id:
            info_parts.append(f"mapped={mapped_id}")
        if detail_reason:
            info_parts.append(f"detail={detail_reason}")

        return {
            "success": False,
            "output": output,
            "error": output,
            "info": "; ".join(info_parts),
        }

    def _convert_params(
        self, definition: dict[str, Any], params: dict[str, Any]
    ) -> dict[str, Any]:
        qgis_params = {}
        param_defs = {p["name"]: p for p in definition.get("parameters", [])}
        param_name_map = {name.lower(): name for name in param_defs.keys()}

        logger.debug("参数定义: %s", param_defs)

        for raw_name, raw_value in params.items():
            canonical_name = raw_name
            if raw_name not in param_defs:
                canonical_name = param_name_map.get(str(raw_name).lower(), raw_name)
                if canonical_name != raw_name:
                    logger.debug("参数名称归一: %s -> %s", raw_name, canonical_name)

            param_def = param_defs.get(canonical_name, {})
            param_type = param_def.get("type", "string")
            value = self._coerce_param_value(param_type, raw_value, param_def)

            if param_type in ("layer", "raster_layer"):
                original_value = value
                value = self._get_layer(value, expected_kind=param_type)
                logger.debug(
                    "图层参数 %s: %s -> %s", canonical_name, original_value, value
                )
            elif param_type == "multi_layer":
                original_value = value
                if isinstance(value, list):
                    value = [self._get_layer(v, expected_kind="layer") for v in value]
                logger.debug(
                    "多图层参数 %s: %s -> %s", canonical_name, original_value, value
                )

            if (
                param_type == "file"
                and value
                and self._is_output_param(canonical_name, param_def)
            ):
                original_value = value
                value = self._resolve_output_path(
                    str(value),
                    canonical_name,
                    self._generate_default_output_name(
                        definition, canonical_name, param_def
                    ),
                    target_subdir="exports",
                )
                logger.debug(
                    "输出路径 %s: %s -> %s", canonical_name, original_value, value
                )

            if (
                param_type in ("output_layer", "output_raster")
                and isinstance(value, str)
                and value.strip()
                and not self._is_temporary_output_ref(value)
            ):
                original_value = value
                value = self._resolve_output_path(
                    value.strip(),
                    canonical_name,
                    self._generate_default_output_name(
                        definition, canonical_name, param_def
                    ),
                    target_subdir="temp",
                )
                logger.debug(
                    "输出图层路径 %s: %s -> %s", canonical_name, original_value, value
                )

            if self._should_normalize_output_path(
                canonical_name, param_type, param_def, value
            ):
                original_value = value
                value = self._resolve_output_path(
                    str(value).strip(),
                    canonical_name,
                    self._generate_default_output_name(
                        definition, canonical_name, param_def
                    ),
                    target_subdir="temp",
                )
                logger.debug(
                    "宽松输出路径归一 %s: %s -> %s",
                    canonical_name,
                    original_value,
                    value,
                )

            qgis_params[canonical_name] = value

        for param_def in definition.get("parameters", []):
            name = param_def["name"]
            if name not in qgis_params:
                param_type = param_def.get("type")
                if param_type in ("output_layer", "output_raster"):
                    qgis_params[name] = "memory:"
                    logger.debug("设置默认输出 %s: memory:", name)
                elif param_type == "file" and self._is_output_param(name, param_def):
                    default_name = self._generate_default_output_name(
                        definition, name, param_def
                    )
                    qgis_params[name] = self._resolve_output_path(
                        None, name, default_name, target_subdir="exports"
                    )
                    logger.debug("设置默认文件输出 %s: %s", name, qgis_params[name])

        logger.debug("转换后的参数: %s", qgis_params)
        return qgis_params

    def _coerce_param_value(
        self, param_type: str, value: Any, param_def: Dict[str, Any]
    ) -> Any:
        if value is None:
            return value

        try:
            if param_type == "boolean":
                if isinstance(value, bool):
                    return value
                if isinstance(value, str):
                    lowered = value.strip().lower()
                    if lowered in ("true", "1", "yes", "y"):
                        return True
                    if lowered in ("false", "0", "no", "n"):
                        return False
                if isinstance(value, (int, float)):
                    return bool(value)
                return value

            if param_type == "integer":
                if isinstance(value, bool):
                    return int(value)
                if isinstance(value, (int, float)):
                    return int(value)
                if isinstance(value, str) and value.strip() != "":
                    return int(float(value))
                return value

            if param_type == "number":
                if isinstance(value, bool):
                    return float(int(value))
                if isinstance(value, (int, float)):
                    return float(value)
                if isinstance(value, str) and value.strip() != "":
                    return float(value)
                return value

            if param_type == "enum":
                enum_values = param_def.get("values", [])
                if not enum_values:
                    return value
                if all(
                    isinstance(v, int) and not isinstance(v, bool) for v in enum_values
                ):
                    if isinstance(value, str) and value.strip() != "":
                        return int(float(value))
                    if isinstance(value, (int, float)):
                        return int(value)
                    return value
                if all(
                    isinstance(v, (int, float)) and not isinstance(v, bool)
                    for v in enum_values
                ):
                    if isinstance(value, str) and value.strip() != "":
                        return float(value)
                    if isinstance(value, (int, float)):
                        return float(value)
                    return value
                return str(value)

            if param_type in ("multi_layer", "fields"):
                if isinstance(value, list):
                    return value
                if isinstance(value, tuple):
                    return list(value)
                if isinstance(value, str):
                    parts = [part.strip() for part in value.split(",") if part.strip()]
                    return parts
                return value
        except Exception as e:
            logger.warning(
                "参数类型转换失败: type=%s, value=%s, error=%s", param_type, value, e
            )
            return value

        return value

    def _is_output_param(self, param_name: str, param_def: Dict[str, Any]) -> bool:
        upper_name = str(param_name).upper()
        if "OUTPUT" in upper_name or upper_name.startswith("DEST"):
            return True

        description = str(param_def.get("description", ""))
        if "输出" in description or "export" in description.lower():
            return True

        return False

    def _is_temporary_output_ref(self, value: str) -> bool:
        marker = value.strip()
        if not marker:
            return False
        upper = marker.upper()
        return marker.startswith("memory:") or upper == "TEMPORARY_OUTPUT"

    def _looks_like_path(self, value: str) -> bool:
        text = str(value or "").strip()
        if not text:
            return False
        if text.startswith(("memory:", "TEMPORARY_OUTPUT")):
            return False
        if "/" in text or "\\" in text:
            return True
        if text.startswith(
            (
                "~",
                ".",
                "autoc",
                "temp",
                "exports",
                "snapshots",
                "intermediate",
                "output",
            )
        ):
            return True

        _, ext = os.path.splitext(text.lower())
        return ext in {
            ".gpkg",
            ".geojson",
            ".json",
            ".shp",
            ".csv",
            ".xlsx",
            ".xls",
            ".tif",
            ".tiff",
            ".png",
            ".jpg",
            ".jpeg",
            ".pdf",
            ".html",
            ".txt",
            ".sqlite",
            ".db",
        }

    def _looks_like_input_path(self, value: str) -> bool:
        text = str(value or "").strip()
        if not text:
            return False
        if os.path.isabs(text):
            return True
        if "/" in text or "\\" in text:
            return True
        lowered = text.lower()
        if lowered.startswith(
            (
                "autoc/",
                "temp/",
                "exports/",
                "snapshots/",
                "intermediate/",
                "output/",
                "./",
                "../",
            )
        ):
            return True
        _, ext = os.path.splitext(lowered)
        return ext in {
            ".gpkg",
            ".geojson",
            ".json",
            ".shp",
            ".dbf",
            ".csv",
            ".tif",
            ".tiff",
            ".vrt",
            ".img",
            ".asc",
        }

    def _should_normalize_output_path(
        self,
        param_name: str,
        param_type: str,
        param_def: Dict[str, Any],
        value: Any,
    ) -> bool:
        if not isinstance(value, str):
            return False
        if not value.strip() or self._is_temporary_output_ref(value):
            return False
        if param_type in ("output_layer", "output_raster", "file"):
            return False
        if not self._is_output_param(param_name, param_def):
            return False
        if param_type not in ("string", "layer", "raster_layer"):
            return False
        return self._looks_like_path(value)

    def _resolve_output_path(
        self,
        user_path: Optional[str],
        param_name: str,
        default_name: Optional[str] = None,
        target_subdir: str = "exports",
    ) -> str:
        try:
            from autocarto.tools.system._shared.output_manager import (
                resolve_output_path,
            )

            if default_name is None:
                default_name = f"output_{param_name}"
            return resolve_output_path(user_path, default_name, target_subdir)
        except ImportError:
            return user_path if user_path else f"/tmp/{default_name or 'output'}"

    def _generate_default_output_name(
        self,
        definition: dict[str, Any],
        param_name: str,
        param_def: Optional[Dict[str, Any]] = None,
    ) -> str:
        tool_name = definition.get("name", "output")
        identifier = " ".join(
            filter(
                None,
                [
                    str(param_name).lower(),
                    str(definition.get("algorithm_id", "")).lower(),
                    str(definition.get("description", "")).lower(),
                    str((param_def or {}).get("description", "")).lower(),
                ],
            )
        )

        extension_map = [
            (("pdf",), ".pdf"),
            (("csv", "timeseries", "time_series"), ".csv"),
            (("gpkg", "geopackage", "package"), ".gpkg"),
            (("dxf",), ".dxf"),
            (("html",), ".html"),
            (("jpg", "jpeg"), ".jpg"),
            (("png", "image", "histogram", "plot", "chart"), ".png"),
        ]

        for keywords, ext in extension_map:
            if any(keyword in identifier for keyword in keywords):
                return f"{tool_name}{ext}"

        return f"{tool_name}_output"

    def _resolve_input_layer_reference(
        self, layer_ref: str, expected_kind: str
    ) -> Dict[str, Any]:
        try:
            from autocarto.tools.system._shared.input_path_resolver import (
                resolve_input_path,
            )

            return resolve_input_path(layer_ref, expected_kind=expected_kind)
        except Exception as e:
            logger.debug("输入路径解析器不可用: %s", e)
            return {
                "status": "missing",
                "resolved_path": None,
                "candidates": [],
                "source": "resolver_unavailable",
                "reason": "resolver_error",
            }

    def _find_layer_by_ref(self, project: Any, layer_ref: str):
        layers = project.mapLayersByName(layer_ref)
        if layers:
            return layers[0]
        layer = project.mapLayer(layer_ref)
        if layer:
            return layer
        return None

    def _get_layer(self, layer_name: Any, expected_kind: str = "layer"):
        if layer_name is None:
            return layer_name

        if not isinstance(layer_name, str):
            return layer_name

        candidate = layer_name.strip()
        if not candidate:
            return layer_name

        try:
            qgis_core = importlib.import_module("qgis.core")
            QgsProject = getattr(qgis_core, "QgsProject")

            project = QgsProject.instance()

            direct = self._find_layer_by_ref(project, candidate)
            if direct is not None:
                return direct

            basename = os.path.splitext(os.path.basename(candidate.replace("\\", "/")))[
                0
            ]
            if basename and basename != candidate:
                by_basename = self._find_layer_by_ref(project, basename)
                if by_basename is not None:
                    logger.debug(
                        "图层参数 basename 匹配: raw=%s matched=%s", candidate, basename
                    )
                    return by_basename

            if not self._looks_like_input_path(candidate):
                return layer_name

            kind = "raster" if expected_kind == "raster_layer" else "vector"
            resolution = self._resolve_input_layer_reference(
                candidate, expected_kind=kind
            )
            status = resolution.get("status")

            if status == "resolved":
                resolved_path = str(resolution.get("resolved_path") or candidate)
                resolved_basename = os.path.splitext(os.path.basename(resolved_path))[0]
                by_resolved_name = self._find_layer_by_ref(project, resolved_basename)
                if by_resolved_name is not None:
                    return by_resolved_name

                logger.debug(
                    "图层输入路径自动修复: raw=%s resolved=%s source=%s",
                    candidate,
                    resolved_path,
                    resolution.get("source"),
                )
                return resolved_path

            if status == "ambiguous":
                preview = ", ".join((resolution.get("candidates") or [])[:3])
                if len(resolution.get("candidates") or []) > 3:
                    preview = f"{preview}, ..."
                raise ValueError(f"输入图层路径存在歧义: {candidate}; 候选: {preview}")

            return layer_name
        except ValueError:
            raise
        except Exception:
            return layer_name

    def _refresh_canvas(self):
        from autocarto.tools.system._shared.canvas_refresh import refresh_canvas

        refresh_canvas(logger=logger)


__all__ = ["ProcessingToolExecutor"]
