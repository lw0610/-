from __future__ import annotations

import os
from typing import Any, Dict, List, Optional, Tuple

SESSION_SUBDIRS = ("temp", "exports", "snapshots", "intermediate", "output", "logs")

_KIND_EXTENSIONS = {
    "vector": (".gpkg", ".shp", ".geojson", ".json", ".dbf", ".csv"),
    "raster": (".tif", ".tiff", ".vrt", ".img", ".asc"),
}


def _split_parts(path: str) -> List[str]:
    normalized = str(path).replace("\\", "/")
    return [part for part in normalized.split("/") if part and part != "."]


def _normalize_expected_kind(expected_kind: Optional[str]) -> str:
    kind = str(expected_kind or "vector").strip().lower()
    if kind == "raster_layer":
        return "raster"
    if kind not in _KIND_EXTENSIONS:
        return "vector"
    return kind


def _normalize_search_dirs(search_dirs: Optional[Dict[str, str]]) -> Dict[str, str]:
    if search_dirs is None:
        return _load_runtime_search_dirs()

    normalized: Dict[str, str] = {}
    for key, value in dict(search_dirs).items():
        if not value:
            continue
        normalized[str(key)] = os.path.abspath(os.path.expanduser(str(value)))

    normalized.setdefault("cwd", os.getcwd())
    if "autoc_root" not in normalized and "project_root" in normalized:
        normalized["autoc_root"] = os.path.join(normalized["project_root"], "autoc")
    return normalized


def _normalize_subdir_name(raw: Optional[str]) -> Optional[str]:
    candidate = str(raw or "").strip().lower()
    if not candidate:
        return None
    if candidate in SESSION_SUBDIRS:
        return candidate
    return None


def _load_runtime_search_dirs() -> Dict[str, str]:
    directories: Dict[str, str] = {"cwd": os.getcwd()}

    try:
        from .output_manager import get_output_subdir, get_project_output_dir

        for subdir in SESSION_SUBDIRS:
            try:
                directories[subdir] = os.path.abspath(get_output_subdir(subdir))
            except Exception:
                continue

        try:
            autoc_root = os.path.abspath(get_project_output_dir())
            directories["autoc_root"] = autoc_root
            directories["project_root"] = os.path.dirname(autoc_root)
        except Exception:
            pass
    except Exception:
        pass

    if "autoc_root" not in directories and "project_root" in directories:
        directories["autoc_root"] = os.path.join(directories["project_root"], "autoc")

    return directories


def _resolve_subdir_base(search_dirs: Dict[str, str], subdir: str) -> Optional[str]:
    normalized_subdir = _normalize_subdir_name(subdir)
    if not normalized_subdir:
        return None

    if normalized_subdir in search_dirs:
        return search_dirs[normalized_subdir]

    autoc_root = search_dirs.get("autoc_root")
    if autoc_root:
        return os.path.join(autoc_root, normalized_subdir)

    project_root = search_dirs.get("project_root")
    if project_root:
        return os.path.join(project_root, "autoc", normalized_subdir)

    return None


def _dedupe_candidates(items: List[Tuple[str, str]]) -> List[Tuple[str, str]]:
    deduped: List[Tuple[str, str]] = []
    seen = set()
    for path, source in items:
        normalized = os.path.abspath(os.path.expanduser(path))
        if normalized in seen:
            continue
        seen.add(normalized)
        deduped.append((normalized, source))
    return deduped


def _build_candidate_paths(
    raw_path: str, search_dirs: Dict[str, str]
) -> List[Tuple[str, str]]:
    expanded = os.path.expanduser(raw_path)
    parts = _split_parts(expanded)
    if not parts:
        return []

    candidates: List[Tuple[str, str]] = []
    if os.path.isabs(expanded):
        candidates.append((os.path.abspath(expanded), "absolute"))
        return candidates

    subdir = ""
    if parts[0] == "autoc":
        normalized_subdir = (
            _normalize_subdir_name(parts[1]) if len(parts) >= 2 else None
        )
        if normalized_subdir:
            subdir = normalized_subdir
            base = _resolve_subdir_base(search_dirs, subdir)
            if base:
                tail = parts[2:]
                path = os.path.join(base, *tail) if tail else base
                candidates.append((path, f"autoc/{subdir}"))
        elif len(parts) >= 4 and parts[1] == "sessions":
            subdir = _normalize_subdir_name(parts[3])
            if not subdir:
                subdir = ""
            base = _resolve_subdir_base(search_dirs, subdir)
            if base:
                tail = parts[4:]
                path = os.path.join(base, *tail) if tail else base
                candidates.append((path, f"autoc/sessions/*/{subdir}"))
        else:
            autoc_root = search_dirs.get("autoc_root")
            if autoc_root:
                candidates.append(
                    (os.path.join(autoc_root, *parts[1:]), "autoc-relative")
                )
    else:
        subdir = _normalize_subdir_name(parts[0])
    if subdir:
        base = _resolve_subdir_base(search_dirs, subdir)
        if base:
            tail = parts[1:]
            path = os.path.join(base, *tail) if tail else base
            candidates.append((path, f"{subdir}-relative"))

    project_root = search_dirs.get("project_root")
    if project_root:
        candidates.append((os.path.join(project_root, *parts), "project-relative"))

    cwd = search_dirs.get("cwd")
    if cwd:
        candidates.append((os.path.join(cwd, *parts), "cwd-relative"))

    return _dedupe_candidates(candidates)


def _collect_existing_paths(path: str, expected_kind: str) -> List[Tuple[str, str]]:
    existing: List[Tuple[str, str]] = []
    normalized = os.path.abspath(os.path.expanduser(path))
    base, ext = os.path.splitext(normalized)
    lowered_ext = ext.lower()

    if os.path.isfile(normalized):
        existing.append((normalized, "exact"))

    if ext:
        if lowered_ext == ".shp" and not os.path.isfile(normalized):
            dbf_path = f"{base}.dbf"
            if os.path.isfile(dbf_path):
                existing.append((dbf_path, "shp->dbf"))
        return existing

    for candidate_ext in _KIND_EXTENSIONS.get(
        expected_kind, _KIND_EXTENSIONS["vector"]
    ):
        candidate = f"{normalized}{candidate_ext}"
        if os.path.isfile(candidate):
            existing.append((candidate, f"auto-ext:{candidate_ext}"))

    return existing


def resolve_input_path(
    raw_path: Any,
    expected_kind: str = "vector",
    search_dirs: Optional[Dict[str, str]] = None,
) -> Dict[str, Any]:
    if not isinstance(raw_path, str):
        return {
            "status": "invalid_path",
            "resolved_path": None,
            "candidates": [],
            "source": "validation",
            "reason": "path_not_string",
        }

    normalized_input = raw_path.strip()
    if not normalized_input:
        return {
            "status": "invalid_path",
            "resolved_path": None,
            "candidates": [],
            "source": "validation",
            "reason": "path_empty",
        }

    kind = _normalize_expected_kind(expected_kind)
    directories = _normalize_search_dirs(search_dirs)
    candidate_paths = _build_candidate_paths(normalized_input, directories)
    if not candidate_paths:
        return {
            "status": "invalid_path",
            "resolved_path": None,
            "candidates": [],
            "source": "candidate_generation",
            "reason": "no_candidate_path",
        }

    resolved_sources: Dict[str, str] = {}
    attempted = [path for path, _ in candidate_paths]
    for candidate_path, candidate_source in candidate_paths:
        for existing_path, match_source in _collect_existing_paths(
            candidate_path, kind
        ):
            resolved_sources.setdefault(
                existing_path, f"{candidate_source}:{match_source}"
            )

    resolved_paths = list(resolved_sources.keys())
    if not resolved_paths:
        return {
            "status": "missing",
            "resolved_path": None,
            "candidates": attempted,
            "source": "lookup",
            "reason": "no_existing_path",
        }

    if len(resolved_paths) > 1:
        return {
            "status": "ambiguous",
            "resolved_path": None,
            "candidates": resolved_paths,
            "source": "lookup",
            "reason": "multiple_candidates",
        }

    resolved_path = resolved_paths[0]
    source = resolved_sources[resolved_path]
    reason = "exact_match" if source.endswith(":exact") else "auto_corrected"
    return {
        "status": "resolved",
        "resolved_path": resolved_path,
        "candidates": [resolved_path],
        "source": source,
        "reason": reason,
    }
