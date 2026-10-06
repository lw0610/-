"""制图任务知识图谱与现有六表渲染契约之间的中间层。"""

from .compiler import (
    CompilationResult,
    TaskKnowledgeCompilationError,
    compile_task_knowledge,
    validate_legacy_route_dataset,
    validate_task_knowledge_for_compile,
)
from .dataset_handoff import (
    CURRENT_SESSION_ARTIFACT_ID,
    CURRENT_SESSION_SOURCE,
    EXPLICIT_SOURCE,
    REQUIRED_ROUTE_TABLES,
    DatasetHandoffError,
    ResolvedRouteDataset,
    resolve_marching_route_dataset,
)
from .extraction import (
    build_extraction_contract,
    build_extraction_prompt,
    extract_and_compile_task_knowledge,
    extract_task_knowledge,
    parse_model_json,
)
from .schema import load_task_knowledge_schema, normalize_task_knowledge
from .validator import ValidationIssue, ValidationReport, validate_task_knowledge

__all__ = [
    "CompilationResult",
    "CURRENT_SESSION_ARTIFACT_ID",
    "CURRENT_SESSION_SOURCE",
    "DatasetHandoffError",
    "EXPLICIT_SOURCE",
    "REQUIRED_ROUTE_TABLES",
    "ResolvedRouteDataset",
    "TaskKnowledgeCompilationError",
    "ValidationIssue",
    "ValidationReport",
    "build_extraction_contract",
    "build_extraction_prompt",
    "compile_task_knowledge",
    "extract_and_compile_task_knowledge",
    "extract_task_knowledge",
    "load_task_knowledge_schema",
    "normalize_task_knowledge",
    "parse_model_json",
    "resolve_marching_route_dataset",
    "validate_legacy_route_dataset",
    "validate_task_knowledge",
    "validate_task_knowledge_for_compile",
]
