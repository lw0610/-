"""图文标注文字的跨平台尺寸计算。

QFontMetricsF 返回的是它实际使用 DPI 下的像素尺寸。将这些
像素换算为布局毫米时，必须使用同一个 DPI，不能写死为 96。
"""

from __future__ import annotations

import math
from typing import Any, List, Optional, Tuple


MM_PER_INCH = 25.4
DEFAULT_FONT_DPI = 96.0


def font_metrics_mm_per_pixel(
    metrics: Any,
    fallback_dpi: float = DEFAULT_FONT_DPI,
) -> float:
    """返回当前字体度量中 1 像素对应的毫米数。"""
    dpi = float(fallback_dpi)
    try:
        candidate = float(metrics.fontDpi())
        if math.isfinite(candidate) and candidate > 0:
            dpi = candidate
    except (AttributeError, TypeError, ValueError):
        pass

    if not math.isfinite(dpi) or dpi <= 0:
        dpi = DEFAULT_FONT_DPI
    return MM_PER_INCH / dpi


def wrap_chinese_text_to_mm(
    text: str,
    metrics: Any,
    max_width_mm: float,
    max_lines: Optional[int] = None,
    ellipsis: bool = True,
    safety_margin_mm: float = 0.8,
) -> Tuple[List[str], float, float]:
    """
    依据字体的实际 DPI 将中英文混排文本按毫米宽度换行。

    ``safety_margin_mm`` 用于吸收 QGIS 布局标签与 Qt 字体替换之间的
    微小测量差异，避免最后一个字越过标注框。
    """
    mm_per_px = font_metrics_mm_per_pixel(metrics)
    usable_width_mm = max(
        0.5,
        float(max_width_mm) - max(0.0, float(safety_margin_mm)),
    )
    max_width_px = max(1.0, usable_width_mm / mm_per_px)
    punctuation = set("，。；：？！》）」』、,.!?:;)")
    lines: List[str] = []
    current = ""
    truncated = False
    source = str(text or "")

    for index, char in enumerate(source):
        if char == "\n":
            lines.append(current)
            current = ""
        else:
            candidate = current + char
            if not current or metrics.horizontalAdvance(candidate) <= max_width_px:
                current = candidate
            else:
                lines.append(current)
                current = char
                # 只在不超宽时才把右标点留在上一行，不以排版规则换取溢出。
                if char in punctuation and lines:
                    previous = lines[-1]
                    if metrics.horizontalAdvance(previous + char) <= max_width_px:
                        lines[-1] = previous + char
                        current = ""

        if max_lines and len(lines) >= max_lines:
            truncated = bool(current) or index < len(source) - 1
            current = ""
            break

    if current and (not max_lines or len(lines) < max_lines):
        lines.append(current)

    if max_lines and len(lines) > max_lines:
        lines = lines[:max_lines]
        truncated = True

    if truncated and ellipsis and lines:
        last = lines[-1]
        while last and metrics.horizontalAdvance(last + "…") > max_width_px:
            last = last[:-1]
        lines[-1] = last + "…"

    line_height_mm = float(metrics.lineSpacing()) * mm_per_px
    return lines, line_height_mm, line_height_mm * len(lines)
