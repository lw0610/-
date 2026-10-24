"""Resolve and validate marching-route datasets across agent handoffs.

The generated dataset is a runtime artifact, not conversational text.  For a
normal narrative task the renderer must resolve the artifact from the current
session instead of trusting an arbitrary path selected by an agent.
"""

from __future__ import annotations

import csv
import json
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any, Dict, Mapping, Optional, Tuple


CURRENT_SESSION_SOURCE = "current_session"
EXPLICIT_SOURCE = "explicit"
CURRENT_SESSION_ARTIFACT_ID = "marching_route_dataset/current_session"

REQUIRED_ROUTE_TABLES: Tuple[str, ...] = (
    "places.csv",
    "routes.csv",
    "route_paths.csv",
    "points.csv",
    "symbol_mapping.csv",
    "callouts.csv",
)


class DatasetHandoffError(ValueError):
    """Stable, machine-readable failure raised before any QGIS mutation."""

    def __init__(
        self,
        code: str,
        message: str,
        *,
        details: Optional[Mapping[str, Any]] = None,
    ):
        super().__init__(message)
        self.code = str(code)
        self.details = dict(details or {})

    def to_dict(self) -> Dict[str, Any]:
        return {
            "error_type": self.code,
            "message": str(self),
            "details": self.details,
        }


@dataclass(frozen=True)
class ResolvedRouteDataset:
    dataset_dir: Path
    dataset_source: str
    artifact_id: str
    counts: Dict[str, int]
    task_id: str = ""
    manifest: Dict[str, Any] = field(default_factory=dict)
    validation: Dict[str, Any] = field(default_factory=dict)

    def to_dict(self) -> Dict[str, Any]:
        return {
            "dataset_dir": str(self.dataset_dir),
            "dataset_source": self.dataset_source,
            "artifact_id": self.artifact_id,
            "counts": dict(self.counts),
            "task_id": self.task_id,
            "manifest_file": (
                str(self.dataset_dir / "compile_manifest.json")
                if self.manifest
                else ""
            ),
            "validation_file": (
                str(self.dataset_dir / "validation_report.json")
                if self.validation
                else ""
            ),
        }


def _read_json_object(path: Path, code: str) -> Dict[str, Any]:
    try:
        payload = json.loads(path.read_text(encoding="utf-8"))
    except Exception as exc:
        raise DatasetHandoffError(
            code,
            f"无法读取或解析数据集元数据：{path}",
            details={"path": str(path), "reason": str(exc)},
        ) from exc
    if not isinstance(payload, dict):
        raise DatasetHandoffError(
            code,
            f"数据集元数据必须是 JSON 对象：{path}",
            details={"path": str(path)},
        )
    return payload


def _count_csv_rows(path: Path) -> int:
    try:
        with path.open("r", encoding="utf-8-sig", newline="") as stream:
            reader = csv.DictReader(stream)
            if not reader.fieldnames:
                raise ValueError("缺少表头")
            return sum(1 for _ in reader)
    except Exception as exc:
        raise DatasetHandoffError(
            "route_dataset_csv_invalid",
            f"无法读取六表数据：{path.name}",
            details={"path": str(path), "reason": str(exc)},
        ) from exc


def _normalize_expected_counts(raw: Any) -> Dict[str, int]:
    if isinstance(raw, str):
        raw_text = raw.strip()
        if not raw_text or raw_text.startswith("{{"):
            return {}
        try:
            raw = json.loads(raw_text)
        except json.JSONDecodeError as exc:
            raise DatasetHandoffError(
                "route_dataset_expected_counts_invalid",
                "expected_table_counts 必须是对象或 JSON 对象",
                details={"value": raw_text},
            ) from exc
    if not isinstance(raw, Mapping):
        return {}
    normalized: Dict[str, int] = {}
    for filename in REQUIRED_ROUTE_TABLES:
        if filename not in raw:
            continue
        try:
            normalized[filename] = int(raw[filename])
        except (TypeError, ValueError) as exc:
            raise DatasetHandoffError(
                "route_dataset_expected_counts_invalid",
                f"预期行数不是整数：{filename}",
                details={"filename": filename, "value": raw[filename]},
            ) from exc
    return normalized


def _resolve_session_dataset(params: Mapping[str, Any]) -> Optional[Path]:
    session_dir = str(params.get("session_dir") or "").strip()
    if not session_dir:
        raw_subdirs = params.get("session_subdirs")
        if isinstance(raw_subdirs, Mapping):
            session_dir = str(raw_subdirs.get("root") or "").strip()
    if not session_dir:
        return None

    session_root = Path(session_dir).expanduser().resolve()
    candidate = (session_root / "intermediate" / "route_knowledge").resolve()
    try:
        candidate.relative_to(session_root)
    except ValueError as exc:
        raise DatasetHandoffError(
            "route_dataset_session_path_invalid",
            "当前会话数据集路径越出了任务目录",
            details={
                "session_dir": str(session_root),
                "dataset_dir": str(candidate),
            },
        ) from exc
    return candidate


def _validate_dataset(
    dataset_dir: Path,
    *,
    require_compilation_metadata: bool,
    expected_counts: Mapping[str, int],
    expected_task_id: str,
) -> Tuple[Dict[str, int], Dict[str, Any], Dict[str, Any]]:
    if not dataset_dir.exists() or not dataset_dir.is_dir():
        raise DatasetHandoffError(
            "route_dataset_dir_missing",
            f"行军路线数据集目录不存在：{dataset_dir}",
            details={"dataset_dir": str(dataset_dir)},
        )

    missing = [
        filename
        for filename in REQUIRED_ROUTE_TABLES
        if not (dataset_dir / filename).is_file()
    ]
    if missing:
        raise DatasetHandoffError(
            "route_dataset_files_missing",
            "行军路线数据集缺少必需的六表文件",
            details={
                "dataset_dir": str(dataset_dir),
                "missing_files": missing,
            },
        )

    actual_counts = {
        filename: _count_csv_rows(dataset_dir / filename)
        for filename in REQUIRED_ROUTE_TABLES
    }

    manifest_path = dataset_dir / "compile_manifest.json"
    validation_path = dataset_dir / "validation_report.json"
    task_graph_path = dataset_dir / "task_graph.json"
    source_path = dataset_dir / "source.txt"
    manifest: Dict[str, Any] = {}
    validation: Dict[str, Any] = {}

    if require_compilation_metadata and not manifest_path.is_file():
        raise DatasetHandoffError(
            "route_dataset_manifest_missing",
            "当前任务的数据集缺少 compile_manifest.json，不能确认其来源",
            details={"dataset_dir": str(dataset_dir)},
        )
    if require_compilation_metadata and not validation_path.is_file():
        raise DatasetHandoffError(
            "route_dataset_validation_missing",
            "当前任务的数据集缺少 validation_report.json，不能安全制图",
            details={"dataset_dir": str(dataset_dir)},
        )
    if require_compilation_metadata and not task_graph_path.is_file():
        raise DatasetHandoffError(
            "route_dataset_task_graph_missing",
            "当前任务的数据集缺少 task_graph.json，不能确认任务来源",
            details={"dataset_dir": str(dataset_dir)},
        )
    if require_compilation_metadata and not source_path.is_file():
        raise DatasetHandoffError(
            "route_dataset_source_file_missing",
            "当前任务的数据集缺少 source.txt，不能追溯原始叙事",
            details={"dataset_dir": str(dataset_dir)},
        )

    if manifest_path.is_file():
        manifest = _read_json_object(
            manifest_path,
            "route_dataset_manifest_invalid",
        )
        manifest_counts = _normalize_expected_counts(manifest.get("counts"))
        mismatches = {
            filename: {
                "manifest": manifest_counts.get(filename),
                "actual": actual_counts[filename],
            }
            for filename in REQUIRED_ROUTE_TABLES
            if manifest_counts.get(filename) != actual_counts[filename]
        }
        if mismatches:
            raise DatasetHandoffError(
                "route_dataset_manifest_count_mismatch",
                "六表实际行数与编译清单不一致，已停止制图",
                details={
                    "dataset_dir": str(dataset_dir),
                    "mismatches": mismatches,
                },
            )

    if validation_path.is_file():
        validation = _read_json_object(
            validation_path,
            "route_dataset_validation_invalid",
        )
        if validation.get("is_valid") is not True:
            raise DatasetHandoffError(
                "route_dataset_validation_failed",
                "当前任务的数据集未通过编译验证，已停止制图",
                details={
                    "dataset_dir": str(dataset_dir),
                    "validation": validation,
                },
            )

    expected_mismatches = {
        filename: {
            "expected": expected_counts[filename],
            "actual": actual_counts[filename],
        }
        for filename in expected_counts
        if actual_counts.get(filename) != expected_counts[filename]
    }
    if expected_mismatches:
        raise DatasetHandoffError(
            "route_dataset_expected_count_mismatch",
            "六表实际行数与上游交接结果不一致，已停止制图",
            details={
                "dataset_dir": str(dataset_dir),
                "mismatches": expected_mismatches,
            },
        )

    manifest_task_id = str(manifest.get("task_id") or "").strip()
    if task_graph_path.is_file():
        task_graph = _read_json_object(
            task_graph_path,
            "route_dataset_task_graph_invalid",
        )
        graph_task = task_graph.get("task")
        graph_task_id = (
            str(graph_task.get("task_id") or "").strip()
            if isinstance(graph_task, Mapping)
            else ""
        )
        if manifest_task_id and graph_task_id != manifest_task_id:
            raise DatasetHandoffError(
                "route_dataset_task_id_mismatch",
                "task_graph.json 与 compile_manifest.json 的 task_id 不一致",
                details={
                    "dataset_dir": str(dataset_dir),
                    "manifest_task_id": manifest_task_id,
                    "task_graph_task_id": graph_task_id,
                },
            )
    if expected_task_id and manifest_task_id != expected_task_id:
        raise DatasetHandoffError(
            "route_dataset_task_mismatch",
            "数据集 task_id 与上游知识抽取任务不一致，已停止制图",
            details={
                "dataset_dir": str(dataset_dir),
                "expected_task_id": expected_task_id,
                "actual_task_id": manifest_task_id,
            },
        )

    return actual_counts, manifest, validation


def resolve_route_dataset(
    params: Mapping[str, Any],
) -> ResolvedRouteDataset:
    """Resolve a trusted dataset without asking another tool for output state."""

    source = str(params.get("dataset_source") or "").strip().lower()
    explicit_raw = str(params.get("dataset_dir") or "").strip()
    session_dataset = _resolve_session_dataset(params)

    if not source:
        if session_dataset is not None and session_dataset.is_dir():
            source = CURRENT_SESSION_SOURCE
        elif explicit_raw:
            # Backward compatibility for a user-supplied legacy six-table set.
            source = EXPLICIT_SOURCE
        elif session_dataset is not None:
            source = CURRENT_SESSION_SOURCE

    if source not in {CURRENT_SESSION_SOURCE, EXPLICIT_SOURCE}:
        raise DatasetHandoffError(
            "route_dataset_source_invalid",
            "dataset_source 必须是 current_session 或 explicit",
            details={"dataset_source": source},
        )

    if source == CURRENT_SESSION_SOURCE:
        if session_dataset is None:
            raise DatasetHandoffError(
                "route_dataset_session_missing",
                "current_session 数据源缺少运行时 session_dir",
            )
        dataset_dir = session_dataset
        if explicit_raw:
            from autocarto.core.paths import resolve_project_path
            supplied = resolve_project_path(explicit_raw)
            if supplied != dataset_dir:
                raise DatasetHandoffError(
                    "route_dataset_handoff_conflict",
                    "传入的 dataset_dir 与当前任务知识抽取结果不一致",
                    details={
                        "current_session_dataset_dir": str(dataset_dir),
                        "supplied_dataset_dir": str(supplied),
                    },
                )
        artifact_id = CURRENT_SESSION_ARTIFACT_ID
        require_metadata = True
    else:
        if not explicit_raw:
            raise DatasetHandoffError(
                "route_dataset_explicit_dir_missing",
                "explicit 数据源必须提供 dataset_dir",
            )
        from autocarto.core.paths import resolve_project_path
        dataset_dir = resolve_project_path(explicit_raw)
        artifact_id = f"marching_route_dataset/explicit:{dataset_dir}"
        require_metadata = False

    expected_counts = _normalize_expected_counts(
        params.get("expected_table_counts")
    )
    expected_task_id = str(params.get("expected_task_id") or "").strip()
    counts, manifest, validation = _validate_dataset(
        dataset_dir,
        require_compilation_metadata=require_metadata,
        expected_counts=expected_counts,
        expected_task_id=expected_task_id,
    )

    return ResolvedRouteDataset(
        dataset_dir=dataset_dir,
        dataset_source=source,
        artifact_id=artifact_id,
        counts=counts,
        task_id=str(manifest.get("task_id") or "").strip(),
        manifest=manifest,
        validation=validation,
    )


# 保留历史导入路径；新绘制工具统一使用中性名称。
resolve_marching_route_dataset = resolve_route_dataset
