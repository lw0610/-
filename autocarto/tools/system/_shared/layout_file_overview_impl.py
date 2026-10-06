from __future__ import annotations

import json
import os
import re
from datetime import datetime
from pathlib import Path
from typing import Any, Dict, List, Optional, Tuple
from urllib import error, request

from qgis.PyQt.QtCore import QRectF
from qgis.PyQt.QtGui import QColor, QFont
from qgis.core import (
    QgsLayoutItemLabel,
    QgsLayoutMeasurement,
    QgsProject,
    QgsUnitTypes,
)

from autocarto.tools.system._shared.canvas_refresh import refresh_canvas
from autocarto.tools.system._shared.layout_runtime import run_in_main_thread

_TEXT_EXTENSIONS = {
    ".txt",
    ".md",
    ".markdown",
    ".csv",
    ".json",
    ".geojson",
    ".yaml",
    ".yml",
    ".xml",
    ".log",
    ".py",
    ".js",
    ".ts",
    ".java",
    ".c",
    ".cpp",
    ".h",
    ".hpp",
}

_VECTOR_EXTENSIONS = {
    ".shp",
    ".shx",
    ".dbf",
    ".geojson",
    ".gpkg",
    ".kml",
    ".gml",
    ".json",
    ".csv",
}

_RASTER_EXTENSIONS = {
    ".tif",
    ".tiff",
    ".img",
    ".jp2",
    ".png",
    ".jpg",
    ".jpeg",
    ".bmp",
}

_SHAPEFILE_SIDECAR_EXTENSIONS = {
    ".shp",
    ".shx",
    ".dbf",
    ".prj",
    ".cpg",
    ".qix",
    ".sbn",
    ".sbx",
}

_ADMIN_KEYWORDS = [
    "县",
    "县级",
    "区",
    "市",
    "乡",
    "镇",
    "行政",
    "边界",
    "区划",
    "county",
    "district",
    "admin",
]

def _http_json_request(
    url: str,
    *,
    method: str = "GET",
    payload: Optional[Dict[str, Any]] = None,
    timeout_sec: float = 12.0,
    headers: Optional[Dict[str, str]] = None,
) -> Any:
    body = None
    merged_headers: Dict[str, str] = {}
    if payload is not None:
        body = json.dumps(payload, ensure_ascii=False).encode("utf-8")
        merged_headers["Content-Type"] = "application/json"
    if headers:
        merged_headers.update(headers)

    req = request.Request(url, data=body, headers=merged_headers, method=method)
    try:
        with request.urlopen(req, timeout=timeout_sec) as resp:
            raw = resp.read()
    except error.HTTPError as exc:
        detail = ""
        try:
            detail = exc.read().decode("utf-8", errors="ignore")
        except Exception:
            detail = ""
        raise RuntimeError(
            f"HTTP {exc.code} when requesting {url}: {detail or exc.reason}"
        ) from exc

    try:
        return json.loads(raw.decode("utf-8"))
    except Exception as exc:
        raise RuntimeError(f"Invalid JSON response from {url}") from exc


def _unwrap_success_envelope(payload: Any) -> Any:
    if isinstance(payload, dict) and payload.get("success") is True:
        return payload.get("data")
    return payload


def _get_runtime_backend_url() -> str:
    try:
        from autocarto.core.service_port_reader import build_runtime_backend_url

        backend_url = build_runtime_backend_url()
    except Exception:
        return ""

    return str(backend_url or "").strip().rstrip("/")


def _load_active_provider_config() -> Dict[str, Any]:
    backend_url = _get_runtime_backend_url()
    if not backend_url:
        raise RuntimeError("Runtime backend URL is unavailable")

    payload = _http_json_request(
        f"{backend_url}/api/config/llm?contract_version=v1",
        headers={"X-AutoCarto-Contract-Version": "v1"},
    )
    llm_cfg = _unwrap_success_envelope(payload)
    if not isinstance(llm_cfg, dict):
        raise RuntimeError("Invalid LLM config payload")

    active_name = str(llm_cfg.get("active") or "").strip()
    providers = llm_cfg.get("providers")
    if not isinstance(providers, list) or not providers:
        raise RuntimeError("No LLM providers configured")

    if active_name:
        for item in providers:
            if not isinstance(item, dict):
                continue
            if str(item.get("name") or "").strip() == active_name:
                return item

    for item in providers:
        if isinstance(item, dict):
            return item
    raise RuntimeError("Cannot resolve active LLM provider")


def _extract_openai_message_content(payload: Any) -> str:
    if not isinstance(payload, dict):
        return ""
    choices = payload.get("choices")
    if not isinstance(choices, list) or not choices:
        return ""
    first = choices[0] if isinstance(choices[0], dict) else {}
    message = first.get("message")
    if not isinstance(message, dict):
        return ""
    content = message.get("content")
    if isinstance(content, str):
        return content
    if isinstance(content, list):
        parts = []
        for part in content:
            if isinstance(part, str):
                parts.append(part)
            elif isinstance(part, dict):
                text = part.get("text")
                if isinstance(text, str):
                    parts.append(text)
        return "".join(parts)
    return ""


def _request_llm_summary(
    *,
    prompt: str,
    system_prompt: str = "你是专业的GIS文件综述助手。",
    max_tokens: int = 220,
    temperature: float = 0.2,
) -> str:
    provider = _load_active_provider_config()
    provider_type = str(provider.get("provider") or "").strip().lower()
    if provider_type != "openai":
        raise RuntimeError(f"Active provider is not openai-compatible: {provider_type}")

    model = str(provider.get("model") or "").strip()
    api_key = str(provider.get("api_key") or "").strip()
    base_url = str(provider.get("base_url") or "").strip().rstrip("/")
    if not base_url:
        base_url = "https://api.openai.com/v1"
    if not model:
        raise RuntimeError("Active model is empty")
    if not api_key:
        raise RuntimeError("Active provider API key is empty")

    payload = {
        "model": model,
        "messages": [
            {"role": "system", "content": system_prompt},
            {"role": "user", "content": prompt},
        ],
        "temperature": temperature,
        "max_tokens": max_tokens,
    }
    response_payload = _http_json_request(
        f"{base_url}/chat/completions",
        method="POST",
        payload=payload,
        timeout_sec=25.0,
        headers={"Authorization": f"Bearer {api_key}"},
    )
    summary = _extract_openai_message_content(response_payload)
    summary = _to_single_paragraph(summary)
    if not summary:
        raise RuntimeError("LLM returned empty summary")
    return summary


def _summarize_with_llm(text: str) -> str:
    source = text.strip()
    if len(source) > 10_000:
        source = source[:10_000]

    prompt = (
        "请基于以下文件内容写一段中文综述（80-150字）。"
        "只输出一段话，不要分点，不要标题，不要换行，不要附加说明。\n\n"
        f"文件内容：\n{source}"
    )
    return _request_llm_summary(prompt=prompt)


def _fmt_size(size: int) -> str:
    value = float(max(0, size))
    for unit in ("B", "KB", "MB", "GB", "TB"):
        if value < 1024 or unit == "TB":
            if unit == "B":
                return f"{int(value)} {unit}"
            return f"{value:.1f} {unit}"
        value /= 1024
    return f"{size} B"


def _decode_text(raw: bytes) -> str:
    for encoding in ("utf-8", "gb18030", "latin-1"):
        try:
            return raw.decode(encoding)
        except Exception:
            continue
    return raw.decode("utf-8", errors="ignore")


def _read_text_excerpt(path: str, max_bytes: int = 120_000) -> str:
    with open(path, "rb") as handle:
        raw = handle.read(max_bytes)
    return _decode_text(raw)


def _summarize_text(text: str, max_chars: int = 320) -> str:
    stripped = text.strip()
    if not stripped:
        return "当前文件未提取到可读文本内容。"

    normalized = re.sub(r"[ \t]+", " ", stripped)
    parts = re.split(r"(?<=[。！？?!])\s+|\n+", normalized)
    picked = []
    current_len = 0
    for part in parts:
        chunk = part.strip()
        if not chunk:
            continue
        if current_len + len(chunk) + 1 > max_chars and picked:
            break
        picked.append(chunk)
        current_len += len(chunk) + 1
        if len(picked) >= 5:
            break

    if not picked:
        picked = [normalized[:max_chars]]
    summary = " ".join(picked).strip()
    if len(summary) > max_chars:
        summary = summary[: max_chars - 3].rstrip() + "..."
    return summary


def _to_single_paragraph(text: str) -> str:
    cleaned = str(text or "").replace("\r", " ").replace("\n", " ")
    cleaned = re.sub(r"\s+", " ", cleaned).strip()
    cleaned = re.sub(r"^\s*[#>*-]+\s*", "", cleaned)
    return cleaned


def _word_wrap(text: str, width: int = 34, max_lines: int = 18) -> str:
    lines = []
    for raw_line in text.splitlines():
        line = raw_line.strip()
        if not line:
            lines.append("")
            continue
        while len(line) > width:
            lines.append(line[:width])
            line = line[width:]
        lines.append(line)

    if len(lines) > max_lines:
        lines = lines[:max_lines]
        if lines[-1]:
            lines[-1] = lines[-1][: max(0, width - 3)] + "..."
        else:
            lines[-1] = "..."
    return "\n".join(lines)


def _set_frame_stroke_width(item: Any, width_mm: float) -> None:
    """
    QGIS/PyQGIS API differs by version:
    - some versions accept float
    - some require QgsLayoutMeasurement
    """
    setter = getattr(item, "setFrameStrokeWidth", None)
    if not callable(setter):
        return

    width = max(0.0, float(width_mm or 0.0))
    try:
        setter(width)
        return
    except Exception:
        pass

    try:
        setter(QgsLayoutMeasurement(width, QgsUnitTypes.LayoutMillimeters))
        return
    except Exception:
        pass

    try:
        setter(QgsLayoutMeasurement(width))
    except Exception:
        pass


def _extract_layer_path(layer_source: str) -> str:
    source = str(layer_source or "").strip()
    if not source:
        return ""
    return source.split("|", 1)[0].strip()


def _path_to_compare_key(path: str) -> str:
    try:
        return os.path.normcase(os.path.abspath(str(path or "")))
    except Exception:
        return str(path or "")


def _is_same_spatial_dataset_path(left: str, right: str) -> bool:
    left_key = _path_to_compare_key(left)
    right_key = _path_to_compare_key(right)
    if not left_key or not right_key:
        return False
    if left_key == right_key:
        return True

    left_path = Path(left_key)
    right_path = Path(right_key)
    if (
        left_path.suffix.lower() in _SHAPEFILE_SIDECAR_EXTENSIONS
        and right_path.suffix.lower() in _SHAPEFILE_SIDECAR_EXTENSIONS
        and left_path.stem.lower() == right_path.stem.lower()
        and left_path.parent == right_path.parent
    ):
        return True
    return False


def _collect_map_context(path: str) -> Dict[str, Any]:
    context: Dict[str, Any] = {
        "active_layer": "",
        "layer_count": 0,
        "layer_names": [],
        "related_layers": [],
    }
    try:
        project = QgsProject.instance()
        layers = list(project.mapLayers().values()) if project is not None else []
    except Exception:
        layers = []

    layer_names: List[str] = []
    related_layers: List[str] = []
    for layer in layers:
        if layer is None:
            continue
        name = ""
        try:
            name_attr = getattr(layer, "name", None)
            name = (
                str(name_attr())
                if callable(name_attr)
                else str(name_attr or "")
            ).strip()
        except Exception:
            name = ""
        if name:
            layer_names.append(name)

        layer_source = ""
        try:
            source_attr = getattr(layer, "source", None)
            layer_source = (
                str(source_attr())
                if callable(source_attr)
                else str(source_attr or "")
            )
        except Exception:
            layer_source = ""
        layer_path = _extract_layer_path(layer_source)
        if layer_path and _is_same_spatial_dataset_path(path, layer_path):
            related_layers.append(name or Path(layer_path).name)

    active_layer_name = ""
    try:
        import qgis.utils

        iface = getattr(qgis.utils, "iface", None)
        if iface is not None:
            active = iface.activeLayer()
            if active is not None:
                active_name_attr = getattr(active, "name", None)
                active_layer_name = (
                    str(active_name_attr())
                    if callable(active_name_attr)
                    else str(active_name_attr or "")
                ).strip()
    except Exception:
        active_layer_name = ""

    context["active_layer"] = active_layer_name
    context["layer_count"] = len(layer_names)
    context["layer_names"] = layer_names[:10]
    context["related_layers"] = related_layers[:6]
    return context


def _contains_any_keyword(source: str, keywords: List[str]) -> bool:
    lowered = source.lower()
    return any(keyword.lower() in lowered for keyword in keywords)


def _infer_region_hint(source_text: str) -> str:
    for region in _REGION_HINTS:
        if region in source_text:
            return region

    match = re.search(r"([\u4e00-\u9fff]{2,12}(?:省|市|自治区|自治州|地区|盟|县|区))", source_text)
    if match:
        return match.group(1)
    return "研究区域"


def _summarize_non_text_rule_based(path: str, context: Dict[str, Any]) -> str:
    stem = Path(path).stem
    ext = Path(path).suffix.lower()
    joined_source = " ".join(
        [stem, context.get("active_layer", "")]
        + list(context.get("related_layers") or [])
        + list(context.get("layer_names") or [])
    )
    region = _infer_region_hint(joined_source)
    has_traffic = _contains_any_keyword(joined_source, _TRAFFIC_KEYWORDS)
    has_admin = _contains_any_keyword(joined_source, _ADMIN_KEYWORDS)

    if has_traffic and has_admin:
        summary = (
            f"该图以{region}县级行政单元为基础，呈现区域交通路线的空间组织特征。"
            "整体路网以县城与重要节点为支撑，主通道与支线衔接形成多方向联系，"
            "可用于识别县域互联互通水平、通道覆盖差异与交通服务重点。"
        )
    elif has_traffic:
        summary = (
            f"该图围绕{region}交通网络进行表达，展示主要通道与关键节点的空间联系。"
            "从图面可观察到干线走廊与支线补充关系，适合用于评估区域通达性、"
            "出行组织与运输联系格局。"
        )
    else:
        spatial_kind = "矢量专题" if ext in _VECTOR_EXTENSIONS else "空间栅格" if ext in _RASTER_EXTENSIONS else "空间数据"
        summary = (
            f"该图基于{region}的{spatial_kind}信息进行综合表达，展示核心地理要素的空间分布与组合关系。"
            "通过图面可快速识别重点区域、结构层次与空间差异，适合用于区域分析、"
            "规划说明和成果汇报场景。"
        )

    layer_count = int(context.get("layer_count") or 0)
    if layer_count > 1:
        summary += f" 当前工程共叠加 {layer_count} 个图层，支持进行多要素对照解读。"
    return summary


def _summarize_non_text_with_llm(
    path: str,
    *,
    stat_info: os.stat_result,
    modified: str,
    context: Dict[str, Any],
) -> str:
    p = Path(path)
    prompt_payload = {
        "file_name": p.name,
        "extension": p.suffix.lower(),
        "file_size": _fmt_size(stat_info.st_size),
        "modified_time": modified,
        "active_layer": context.get("active_layer", ""),
        "related_layers": context.get("related_layers", []),
        "layer_count": context.get("layer_count", 0),
        "layer_names": context.get("layer_names", []),
    }
    prompt = (
        "请根据下面的地图文件元数据与工程上下文，生成一段中文“数据综述”（90-160字）。"
        "要求：突出空间格局和专题含义；"
        "不要输出“无法解析”“非文本文件”等技术提示；只输出一段话。\n\n"
        f"{json.dumps(prompt_payload, ensure_ascii=False)}"
    )
    return _request_llm_summary(
        prompt=prompt,
        max_tokens=260,
        temperature=0.3,
    )


def _infer_current_file_path() -> str:
    try:
        import qgis.utils

        iface = getattr(qgis.utils, "iface", None)
        if iface is not None:
            active_layer = iface.activeLayer()
            if active_layer is not None:
                path = _extract_layer_path(active_layer.source())
                if path and os.path.exists(path):
                    return os.path.abspath(path)
    except Exception:
        pass

    try:
        project = QgsProject.instance()
        for layer in project.mapLayers().values():
            if layer is None or not hasattr(layer, "source"):
                continue
            path = _extract_layer_path(layer.source())
            if path and os.path.exists(path):
                return os.path.abspath(path)
    except Exception:
        pass

    return ""


def _resolve_file_path(params: Dict[str, Any]) -> str:
    for key in ("file_path", "path", "current_file", "source_path", "input_path"):
        raw = str(params.get(key) or "").strip()
        if not raw:
            continue
        expanded = os.path.expanduser(raw)
        if os.path.isabs(expanded) and os.path.exists(expanded):
            return os.path.abspath(expanded)

        candidate = os.path.abspath(expanded)
        if os.path.exists(candidate):
            return candidate

        try:
            project_file = QgsProject.instance().fileName()
            if project_file:
                project_dir = os.path.dirname(project_file)
                project_candidate = os.path.abspath(os.path.join(project_dir, expanded))
                if os.path.exists(project_candidate):
                    return project_candidate
        except Exception:
            pass

    return _infer_current_file_path()


def _build_overview_text(path: str, params: Dict[str, Any]) -> Tuple[str, str]:
    p = Path(path)
    stat_info = p.stat()
    modified = datetime.fromtimestamp(stat_info.st_mtime).strftime("%Y-%m-%d %H:%M")
    ext = p.suffix.lower()

    summary_override = str(params.get("summary") or "").strip()
    if summary_override:
        summary_body = summary_override
    elif ext in _TEXT_EXTENSIONS:
        try:
            text_excerpt = _read_text_excerpt(path)
            try:
                summary_body = _summarize_with_llm(text_excerpt)
            except Exception:
                summary_body = _summarize_text(text_excerpt)
        except Exception as exc:
            summary_body = f"文本解析失败：{exc}"
    else:
        context = _collect_map_context(path)
        try:
            summary_body = _summarize_non_text_with_llm(
                path,
                stat_info=stat_info,
                modified=modified,
                context=context,
            )
        except Exception:
            summary_body = _summarize_non_text_rule_based(path, context)

    summary_body = _to_single_paragraph(summary_body)
    title = str(params.get("title") or "").strip() or f"数据综述：{p.name}"
    metadata_text = "；".join(
        [
            f"类型：{ext or '(无扩展名)'}",
            f"大小：{_fmt_size(stat_info.st_size)}",
            f"修改时间：{modified}",
        ]
    )
    overview_paragraph = f"{metadata_text}。{summary_body}".strip()
    overview_text = f"{title}\n{overview_paragraph}"
    return title, overview_text


def _remove_existing_item(layout, item_id: str) -> None:
    if not item_id:
        return
    existing = layout.itemById(item_id)
    if existing is not None:
        layout.removeLayoutItem(existing)


def add_file_overview_to_layout_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    def _execute() -> Dict[str, Any]:
        try:
            layout_name = str(params.get("layout") or "").strip()
            if not layout_name:
                return {
                    "success": False,
                    "output": "",
                    "info": "Missing required parameter: layout",
                }

            project = QgsProject.instance()
            layout = project.layoutManager().layoutByName(layout_name)
            if not layout:
                return {
                    "success": False,
                    "output": "",
                    "info": f"Layout not found: {layout_name}",
                }

            resolved_path = _resolve_file_path(params)
            if not resolved_path or not os.path.exists(resolved_path):
                return {
                    "success": False,
                    "output": "",
                    "info": "Cannot resolve current file. Provide file_path or load/select a layer first.",
                }

            _, overview_text = _build_overview_text(resolved_path, params)

            panel_x = float(params.get("x", 150) or 150)
            panel_y = float(params.get("y", 10) or 10)
            panel_width = float(params.get("width", 130) or 130)
            panel_height = float(params.get("height", 180) or 180)
            text_height = max(30.0, panel_height)

            label_item_id = str(params.get("label_item_id") or "file_overview_label")
            replace_existing = bool(params.get("replace_existing", True))

            if replace_existing:
                _remove_existing_item(layout, label_item_id)
                _remove_existing_item(
                    layout, str(params.get("picture_item_id") or "file_overview_picture")
                )

            label_item = QgsLayoutItemLabel(layout)
            label_item.setId(label_item_id)
            label_item.attemptSetSceneRect(
                QRectF(panel_x, panel_y, panel_width, text_height)
            )
            label_item.setText(_word_wrap(overview_text, width=40, max_lines=22))

            font_size = int(params.get("font_size", 8) or 8)
            font = QFont(str(params.get("font_family") or "Microsoft YaHei"), font_size)
            label_item.setFont(font)
            if hasattr(label_item, "setFontColor"):
                label_item.setFontColor(
                    QColor(str(params.get("font_color") or "#111827"))
                )
            if hasattr(label_item, "setMargin"):
                label_item.setMargin(float(params.get("text_margin", 1.6) or 1.6))

            label_item.setBackgroundEnabled(True)
            label_item.setBackgroundColor(
                QColor(str(params.get("background_color") or "#ffffff"))
            )
            label_item.setFrameEnabled(True)
            label_item.setFrameStrokeColor(
                QColor(str(params.get("frame_color") or "#f59e0b"))
            )
            _set_frame_stroke_width(
                label_item, float(params.get("frame_width", 0.45) or 0.45)
            )
            layout.addLayoutItem(label_item)

            refresh_canvas()
            return {
                "success": True,
                "output": label_item_id,
                "info": f"File overview panel added to layout '{layout_name}'",
                "data": {
                    "layout": layout_name,
                    "file_path": resolved_path,
                    "label_item_id": label_item_id,
                    "panel_rect_mm": {
                        "x": panel_x,
                        "y": panel_y,
                        "width": panel_width,
                        "height": panel_height,
                    },
                    "overview_text": overview_text,
                },
            }
        except Exception as exc:
            return {
                "success": False,
                "output": "",
                "info": f"layout_add_file_overview failed: {exc}",
            }

    return run_in_main_thread(_execute)
