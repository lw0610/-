from pathlib import Path
from typing import Any, Dict


def resolve_project_identity(raw_title: Any, raw_file_path: Any) -> Dict[str, str]:
    title = str(raw_title or "").strip()
    file_path = str(raw_file_path or "").strip()

    file_stem = ""
    if file_path:
        try:
            file_stem = Path(file_path).stem.strip()
        except Exception:
            file_stem = ""

    if title:
        project_name = title
        title_source = "project_title"
    elif file_stem:
        project_name = file_stem
        title_source = "file_name"
    else:
        project_name = "未命名项目"
        title_source = "unsaved_default"

    return {
        "title": title,
        "file_path": file_path,
        "project_name": project_name,
        "display_name": project_name,
        "title_source": title_source,
    }
