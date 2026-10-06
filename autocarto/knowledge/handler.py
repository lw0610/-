"""AutoCarto 工具层的薄适配器；不在 QGIS GUI 线程内调用大模型。"""

from __future__ import annotations

import copy
import hashlib
import json
import shutil
import tempfile
from pathlib import Path
from typing import Any, Dict, Tuple

from .compiler import (
    TaskKnowledgeCompilationError,
    compile_task_knowledge,
    validate_task_knowledge_for_compile,
)
from .extraction import build_extraction_contract, parse_model_json
from .review import ReviewContextError, verify_review_context


def _to_bool(value: Any) -> bool:
    if isinstance(value, bool):
        return value
    return str(value or "").strip().lower() in {"1", "true", "yes", "y", "on"}


def _resolve_compile_output_dir(
    params: Dict[str, Any],
) -> Tuple[str, str, bool, bool]:
    """Return output_dir, storage_scope, auto_overwrite, cleanup_on_failure."""

    explicit_output_dir = str(params.get("output_dir") or "").strip()
    if explicit_output_dir:
        return explicit_output_dir, "explicit", False, False

    session_dir = str(params.get("session_dir") or "").strip()
    raw_subdirs = params.get("session_subdirs")
    if not session_dir and isinstance(raw_subdirs, dict):
        session_dir = str(raw_subdirs.get("root") or "").strip()

    if session_dir:
        session_root = Path(session_dir).expanduser().resolve()
        intermediate_dir = (session_root / "intermediate").resolve()
        if isinstance(raw_subdirs, dict):
            supplied_intermediate = str(
                raw_subdirs.get("intermediate") or ""
            ).strip()
            if supplied_intermediate:
                candidate = Path(supplied_intermediate).expanduser()
                if not candidate.is_absolute():
                    candidate = session_root / candidate
                candidate = candidate.resolve()
                # Runtime session subdirectories must never redirect writes outside
                # the current task. A stale or malformed value falls back safely.
                if candidate == intermediate_dir:
                    intermediate_dir = candidate

        output_dir = intermediate_dir / "route_knowledge"
        # This directory is the latest successful extraction for the task. The
        # compiler replaces it atomically, preserving the old copy on failure.
        return str(output_dir), "session_intermediate", True, False

    # Direct/offline calls have no task session; preserve the old temporary fallback.
    output_dir = tempfile.mkdtemp(prefix="autocarto-route-knowledge-")
    return output_dir, "temporary_fallback", False, True


def get_marching_route_extraction_contract_handler(
    params: Dict[str, Any],
) -> Dict[str, Any]:
    """向运行时知识代理公开与离线抽取管线相同的正式契约。"""

    try:
        compact = _to_bool(params.get("compact", False))
        payload = build_extraction_contract(
            str(params.get("source_text") or ""), compact=compact
        )
    except ValueError as exc:
        return {
            "success": False,
            "output": "",
            "info": str(exc),
            "error": json.dumps(
                {
                    "error_type": "task_knowledge_source_missing",
                    "message": str(exc),
                },
                ensure_ascii=False,
            ),
            "error_type": "task_knowledge_source_missing",
        }
    # Capability metadata is not part of the extraction contract identity.
    payload["review_protocol_version"] = 1
    if compact:
        # These fields are injected by the runtime, not exposed as tool inputs.
        # Report only the root; the caller owns its extraction subdirectory.
        session_dir = str(params.get("session_dir") or "").strip()
        subdirs = params.get("session_subdirs")
        if not session_dir and isinstance(subdirs, dict):
            session_dir = str(subdirs.get("root") or "").strip()
        if session_dir:
            payload["runtime_session_dir"] = str(
                Path(session_dir).expanduser().resolve()
            )
    return {
        "success": True,
        "output": payload,
        "info": "已生成行军路线任务知识图谱抽取契约",
        "data": payload,
    }


def _pre_geocoding_contract_id(
    current_contract: Dict[str, Any], source_text: str
) -> str:
    """Recognize only the exact schema before optional lookup metadata existed.

    The caller supplies no historical schema or compatibility flag. Rebuilding
    this one predecessor from the installed schema means any other contract
    change still invalidates old drafts instead of bypassing schema identity.
    """

    previous_schema = copy.deepcopy(current_contract["json_schema"])
    entity = previous_schema["properties"]["spatial_entities"]["items"]
    field = entity["properties"].get("geocoding")
    if (
        not isinstance(field, dict)
        or field.get("type") != "object"
        or field.get("additionalProperties") is not True
        or set(field) - {"type", "description", "additionalProperties"}
        or "geocoding" in entity.get("required", [])
    ):
        return ""
    del entity["properties"]["geocoding"]
    canonical_schema = json.dumps(
        previous_schema, ensure_ascii=False, sort_keys=True, separators=(",", ":")
    )
    digest = hashlib.sha256(
        (canonical_schema + "\0" + source_text).encode("utf-8")
    ).hexdigest()
    return f"tkg-v1-sha256:{digest}"


def compile_marching_route_dataset_handler(
    params: Dict[str, Any],
) -> Dict[str, Any]:
    source_text = str(params.get("source_text") or "")
    try:
        expected_contract = build_extraction_contract(source_text)
    except ValueError as exc:
        error_payload = {
            "error_type": "task_knowledge_source_missing",
            "message": str(exc),
        }
        return {
            "success": False,
            "output": "",
            "info": str(exc),
            "error": json.dumps(error_payload, ensure_ascii=False),
            "error_type": error_payload["error_type"],
        }
    contract_id = str(params.get("contract_id") or "").strip()
    # Existing saved reviews have the preceding schema hash. They may receive
    # coordinates without re-extraction, but only an authenticated review can
    # use this exact additive migration. verify_review_context below binds the
    # source and complete current graph before validation or file writes.
    compatible_review = (
        "review_context" in params
        and bool(contract_id)
        and contract_id == _pre_geocoding_contract_id(expected_contract, source_text)
    )
    if contract_id != expected_contract["contract_id"] and not compatible_review:
        error_payload = {
            "error_type": "task_knowledge_contract_mismatch",
            "message": (
                "contract_id 缺失或与 source_text/schema 不匹配；"
                "请先调用 marching_route_get_extraction_contract"
            ),
        }
        return {
            "success": False,
            "output": "",
            "info": error_payload["message"],
            "error": json.dumps(error_payload, ensure_ascii=False),
            "error_type": error_payload["error_type"],
        }

    cleanup_output_dir = False
    output_dir = ""
    try:
        raw_graph = params.get("knowledge_json")
        if raw_graph is None:
            raw_graph = params.get("task_graph_json")
        graph = parse_model_json(raw_graph)
        review = None
        if "review_context" in params:
            review = verify_review_context(
                params["review_context"], graph, source_text
            )
        if _to_bool(params.get("validate_only", False)):
            # A validation request never allocates or replaces an output directory.
            # Invalid fields are a successful validation response, not a runtime
            # failure: the editor needs their exact paths while keeping its draft.
            report = validate_task_knowledge_for_compile(
                graph, source_text=source_text, review=review
            )
            validation = report.to_dict()
            validation.update({
                "valid": report.is_valid,
                "errors": [issue.to_dict() for issue in report.errors],
                "warnings": [issue.to_dict() for issue in report.warnings],
            })
            payload = {"validation": validation, "review_protocol_version": 1}
            return {
                "success": True,
                "output": payload,
                "data": payload,
                "info": (
                    "任务知识图谱校验完成"
                    if report.is_valid
                    else "请修改标记的字段后再确认"
                ),
            }
        (
            output_dir,
            storage_scope,
            auto_overwrite,
            cleanup_output_dir,
        ) = _resolve_compile_output_dir(params)
        result = compile_task_knowledge(
            graph,
            output_dir,
            source_text=source_text,
            template_dataset_dir=(
                str(params.get("template_dataset_dir") or "").strip() or None
            ),
            symbol_assets_dir=(
                str(params.get("symbol_assets_dir") or "").strip() or None
            ),
            overwrite=_to_bool(params.get("overwrite", False)) or auto_overwrite,
            review=review,
        )
        payload = result.to_dict()
        task = graph.get("task") if isinstance(graph, dict) else {}
        task_id = (
            str(task.get("task_id") or "").strip()
            if isinstance(task, dict)
            else ""
        )
        dataset_source = (
            "current_session"
            if storage_scope == "session_intermediate"
            else "explicit"
        )
        artifact_id = (
            "marching_route_dataset/current_session"
            if storage_scope == "session_intermediate"
            else f"marching_route_dataset/explicit:{Path(output_dir).resolve()}"
        )
        payload.update(
            {
                "results_root": str(Path(output_dir).resolve()),
                "storage_scope": storage_scope,
                "persistent": storage_scope != "temporary_fallback",
                "dataset_source": dataset_source,
                "artifact_id": artifact_id,
                "task_id": task_id,
                "table_counts": dict(result.counts),
            }
        )
        payload["handoff"] = {
            "dataset_source": dataset_source,
            "artifact_id": artifact_id,
            "dataset_dir": payload["dataset_dir"],
            "task_id": task_id,
            "task_graph_file": payload["task_graph_file"],
            "source_file": payload["source_file"],
            "table_counts": dict(result.counts),
            "validation": payload["validation"],
            "warnings": list(payload["warnings"]),
        }
        return {
            "success": True,
            "output": payload,
            "info": (
                "任务知识图谱已校验并编译为六表数据集；"
                f"结果已保存到 {payload['dataset_dir']}"
            ),
            "data": payload,
        }
    except ReviewContextError as exc:
        return {
            "success": False,
            "output": "",
            "info": str(exc),
            "error": json.dumps(
                {"error_type": "task_knowledge_review_invalid", "message": str(exc)},
                ensure_ascii=False,
            ),
            "error_type": "task_knowledge_review_invalid",
        }
    except TaskKnowledgeCompilationError as exc:
        if cleanup_output_dir:
            shutil.rmtree(Path(output_dir), ignore_errors=True)
        error_payload = {
            "error_type": "task_knowledge_validation_failed",
            "message": str(exc),
            "validation": exc.report.to_dict(),
        }
        return {
            "success": False,
            "output": "",
            "info": str(exc),
            "error": json.dumps(error_payload, ensure_ascii=False),
            "error_type": "task_knowledge_validation_failed",
            "data": {
                "validation": exc.report.to_dict(),
            },
        }
    except Exception as exc:
        if cleanup_output_dir:
            shutil.rmtree(Path(output_dir), ignore_errors=True)
        error_payload = {
            "error_type": "task_knowledge_compile_failed",
            "message": f"任务知识图谱编译失败：{exc}",
        }
        return {
            "success": False,
            "output": "",
            "info": error_payload["message"],
            "error": json.dumps(error_payload, ensure_ascii=False),
            "error_type": "task_knowledge_compile_failed",
        }
