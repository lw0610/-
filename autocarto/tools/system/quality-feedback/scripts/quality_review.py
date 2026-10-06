#!/usr/bin/env python3
"""Prepare QGIS maps for multimodal review and load the final comparison.

The script implements AutoCarto's custom CLI contract: it reads one JSON
object from stdin and writes one JSON result to stdout.  It never calls an
image-generation API.  Its only responsibilities are protecting the original
QGIS export, recording review context, and attaching local images through the
``_agent_media`` envelope understood by the agent runtime.
"""

from __future__ import annotations

import hashlib
import json
import math
import os
import stat
import struct
import sys
import uuid
import zlib
from contextlib import contextmanager
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Dict, List, Mapping, Tuple


MEDIA_KEY = "_agent_media"
MANIFEST_SCHEMA_VERSION = "quality-feedback/v3"
SUPPORTED_MANIFEST_SCHEMA_VERSIONS = {
    "quality-feedback/v2",
    MANIFEST_SCHEMA_VERSION,
}
LAYOUT_SNAPSHOT_SCHEMA_VERSION = "quality-feedback-layout-snapshot/v1"
MAX_MULTIMODAL_REVIEW_PIXELS = 16_000_000
MAX_PNG_PIXELS = MAX_MULTIMODAL_REVIEW_PIXELS
MAX_IMAGE_BYTES = 50 * 1024 * 1024
MAX_LAYOUT_SNAPSHOT_BYTES = 10 * 1024 * 1024


def _absolute_path(path: Path) -> Path:
    """Return an absolute normalized path without following symlinks."""

    return Path(os.path.abspath(os.fspath(path.expanduser())))


def _read_regular_file(
    path: Path, *, label: str, max_bytes: int = MAX_IMAGE_BYTES
) -> bytes:
    if not path.is_absolute():
        raise ValueError(f"{label}路径必须是绝对路径: {path}")
    if path.is_symlink():
        raise ValueError(f"{label}不能是符号链接: {path}")

    flags = os.O_RDONLY | getattr(os, "O_NOFOLLOW", 0)
    try:
        descriptor = os.open(path, flags)
    except FileNotFoundError as exc:
        raise FileNotFoundError(f"{label}不存在: {path}") from exc
    except OSError as exc:
        raise ValueError(f"无法安全读取{label}: {path}") from exc
    try:
        file_stat = os.fstat(descriptor)
        if not stat.S_ISREG(file_stat.st_mode):
            raise ValueError(f"{label}必须是普通文件: {path}")
        if file_stat.st_size > max_bytes:
            limit_mb = max_bytes / (1024 * 1024)
            raise ValueError(f"{label}超过 {limit_mb:g} MB 上限: {path}")
        with os.fdopen(descriptor, "rb", closefd=False) as handle:
            return handle.read()
    finally:
        os.close(descriptor)


def _png_metadata(data: bytes) -> Tuple[int, int, Dict[str, Any] | None]:
    if not data.startswith(b"\x89PNG\r\n\x1a\n"):
        raise ValueError("PNG 签名无效")

    width = height = bit_depth = color_type = 0
    dpi: Dict[str, Any] | None = None
    saw_ihdr = False
    saw_plte = False
    saw_idat = False
    ended_idat = False
    saw_iend = False
    idat_parts: List[bytes] = []
    offset = 8

    while offset + 12 <= len(data):
        length = struct.unpack(">I", data[offset : offset + 4])[0]
        chunk_type = data[offset + 4 : offset + 8]
        if len(chunk_type) != 4 or any(
            not (65 <= value <= 90 or 97 <= value <= 122) for value in chunk_type
        ):
            raise ValueError("PNG 数据块类型无效")
        if not 65 <= chunk_type[2] <= 90:
            raise ValueError("PNG 数据块保留位无效")
        is_critical = 65 <= chunk_type[0] <= 90
        if is_critical and chunk_type not in {b"IHDR", b"PLTE", b"IDAT", b"IEND"}:
            raise ValueError(
                f"PNG 包含未知关键数据块: {chunk_type.decode('ascii')}"
            )

        payload_start = offset + 8
        payload_end = payload_start + length
        if payload_end + 4 > len(data):
            raise ValueError("PNG 数据块被截断")
        payload = data[payload_start:payload_end]
        expected_crc = struct.unpack(">I", data[payload_end : payload_end + 4])[0]
        actual_crc = zlib.crc32(chunk_type + payload) & 0xFFFFFFFF
        if expected_crc != actual_crc:
            raise ValueError(
                f"PNG {chunk_type.decode('ascii', errors='replace')} 数据块 CRC 无效"
            )

        if not saw_ihdr:
            if chunk_type != b"IHDR" or length != 13:
                raise ValueError("PNG 首个数据块必须是完整 IHDR")
            (
                width,
                height,
                bit_depth,
                color_type,
                compression,
                filter_method,
                interlace,
            ) = struct.unpack(">IIBBBBB", payload)
            if width <= 0 or height <= 0 or width * height > MAX_PNG_PIXELS:
                raise ValueError(
                    "PNG 尺寸无效或超过千问单图 1600 万像素限制；"
                    "请按页面规格使用 A0=100、A1=140、A2/A3/A4=200 DPI 导出"
                )
            if compression != 0 or filter_method != 0 or interlace != 0:
                raise ValueError("PNG 压缩、过滤或隔行参数不受支持；请使用 QGIS 非隔行 PNG 导出")
            valid_depths = {
                0: {1, 2, 4, 8, 16},
                2: {8, 16},
                3: {1, 2, 4, 8},
                4: {8, 16},
                6: {8, 16},
            }
            if bit_depth not in valid_depths.get(color_type, set()):
                raise ValueError("PNG 位深与颜色类型组合无效")
            saw_ihdr = True
        elif chunk_type == b"IHDR":
            raise ValueError("PNG 包含重复 IHDR")
        elif chunk_type == b"PLTE":
            if saw_plte or saw_idat:
                raise ValueError("PNG PLTE 必须唯一且位于 IDAT 之前")
            if color_type in {0, 4}:
                raise ValueError("该 PNG 颜色类型不允许 PLTE")
            if length == 0 or length % 3 or length > 768:
                raise ValueError("PNG PLTE 长度无效")
            if color_type == 3 and length // 3 > 2**bit_depth:
                raise ValueError("PNG PLTE 颜色数超过位深限制")
            saw_plte = True
        elif chunk_type == b"pHYs" and length == 9:
            if saw_idat:
                raise ValueError("PNG pHYs 必须位于 IDAT 之前")
            x_ppm, y_ppm, unit = struct.unpack(">IIB", payload)
            if unit == 1:
                dpi = {
                    "x": round(x_ppm * 0.0254, 3),
                    "y": round(y_ppm * 0.0254, 3),
                    "source": "png_pHYs",
                }
        elif chunk_type == b"IDAT":
            if ended_idat:
                raise ValueError("PNG IDAT 数据块必须连续")
            saw_idat = True
            idat_parts.append(payload)
        elif saw_idat and chunk_type != b"IEND":
            ended_idat = True

        if chunk_type == b"IEND":
            if length != 0:
                raise ValueError("PNG IEND 数据块无效")
            saw_iend = True
            offset = payload_end + 4
            break
        offset = payload_end + 4

    if not saw_ihdr or not idat_parts or not saw_iend:
        raise ValueError("PNG 必须包含完整 IHDR、IDAT 和 IEND")
    if color_type == 3 and not saw_plte:
        raise ValueError("索引色 PNG 必须包含 PLTE")
    if offset != len(data):
        raise ValueError("PNG IEND 之后存在多余或截断数据")
    channels = {0: 1, 2: 3, 3: 1, 4: 2, 6: 4}[color_type]
    row_bytes = (width * channels * bit_depth + 7) // 8
    expected_decoded_bytes = (row_bytes + 1) * height
    compressed = b"".join(idat_parts)
    try:
        decompressor = zlib.decompressobj()
        decoded = decompressor.decompress(compressed, expected_decoded_bytes + 1)
    except zlib.error as exc:
        raise ValueError("PNG IDAT 压缩数据无效") from exc
    if decompressor.unconsumed_tail or len(decoded) > expected_decoded_bytes:
        raise ValueError("PNG IDAT 解压数据超过由 IHDR 声明的尺寸")
    if not decompressor.eof:
        raise ValueError("PNG IDAT 压缩流不完整")
    if decompressor.unused_data:
        raise ValueError("PNG IDAT 压缩流后存在多余数据")
    if len(decoded) != expected_decoded_bytes:
        raise ValueError("PNG IDAT 解压长度与 IHDR 声明不一致")
    for row_index in range(height):
        filter_type = decoded[row_index * (row_bytes + 1)]
        if filter_type > 4:
            raise ValueError("PNG 扫描行包含无效过滤器")
    return width, height, dpi


def _detect_image(path: Path) -> Tuple[bytes, str]:
    data = _read_regular_file(path, label="图片")
    if not data.startswith(b"\x89PNG\r\n\x1a\n"):
        raise ValueError(f"质量反馈流程仅接受 PNG 图片: {path}")
    _png_metadata(data)
    return data, "image/png"


def _inspect_image_bytes(data: bytes, mime_type: str | None = None) -> Dict[str, Any]:
    resolved_mime = mime_type or "image/png"
    if resolved_mime != "image/png" or not data.startswith(b"\x89PNG\r\n\x1a\n"):
        raise ValueError("质量反馈流程仅接受 PNG 图片")
    width, height, dpi = _png_metadata(data)
    return {
        "mime_type": resolved_mime,
        "byte_size": len(data),
        "width": width,
        "height": height,
        "dpi": dpi,
        "sha256": hashlib.sha256(data).hexdigest(),
    }


def _string_list(value: Any) -> List[str]:
    if not isinstance(value, list):
        return []
    return [str(item).strip() for item in value if str(item).strip()]


def _string_leaves(value: Any) -> List[str]:
    leaves: List[str] = []
    if isinstance(value, Mapping):
        for nested in value.values():
            leaves.extend(_string_leaves(nested))
    elif isinstance(value, list):
        for nested in value:
            leaves.extend(_string_leaves(nested))
    elif isinstance(value, str) and value.strip():
        leaves.append(value.strip())
    return leaves


def _finite_number(value: Any, *, label: str) -> float:
    if isinstance(value, bool):
        raise ValueError(f"{label} 必须是有限数值")
    try:
        number = float(value)
    except (TypeError, ValueError) as exc:
        raise ValueError(f"{label} 必须是有限数值") from exc
    if not math.isfinite(number):
        raise ValueError(f"{label} 必须是有限数值")
    return number


def _validate_layout_item_state(
    layout_item_state: Mapping[str, Any],
    *,
    layout_name: str,
    handed_off_ids: List[str],
) -> None:
    state_layout_name = str(
        layout_item_state.get("layout")
        or layout_item_state.get("layout_name")
        or ""
    ).strip()
    if state_layout_name and state_layout_name != layout_name:
        raise ValueError("layout_item_state 的布局名称与 layout_name 不一致")

    page_size = layout_item_state.get("page_size_mm")
    if not isinstance(page_size, Mapping):
        raise ValueError("layout_item_state 缺少 page_size_mm")
    for key in ("width", "height"):
        if _finite_number(page_size.get(key), label=f"page_size_mm.{key}") <= 0:
            raise ValueError(f"page_size_mm.{key} 必须大于 0")

    missing_item_ids = layout_item_state.get("missing_item_ids")
    if isinstance(missing_item_ids, list) and missing_item_ids:
        raise ValueError(
            "layout_item_state 仍有未找到的元素: "
            + ", ".join(str(value) for value in missing_item_ids)
        )

    state_items = layout_item_state.get("items")
    if not isinstance(state_items, Mapping):
        raise ValueError("layout_item_state 缺少 items")
    missing_state_ids = [item_id for item_id in handed_off_ids if item_id not in state_items]
    if missing_state_ids:
        raise ValueError(
            "layout_item_state 缺少已交接元素状态: " + ", ".join(missing_state_ids)
        )

    for item_id in handed_off_ids:
        item_state = state_items.get(item_id)
        if not isinstance(item_state, Mapping):
            raise ValueError(f"layout_item_state.items.{item_id} 必须是对象")
        if not str(item_state.get("type") or "").strip():
            raise ValueError(f"layout_item_state.items.{item_id}.type 不能为空")
        rect = item_state.get("rect_mm")
        if not isinstance(rect, Mapping):
            raise ValueError(f"layout_item_state.items.{item_id} 缺少 rect_mm")
        for key in ("x", "y", "width", "height"):
            number = _finite_number(
                rect.get(key), label=f"layout_item_state.items.{item_id}.rect_mm.{key}"
            )
            if key in {"width", "height"} and number <= 0:
                raise ValueError(
                    f"layout_item_state.items.{item_id}.rect_mm.{key} 必须大于 0"
                )


def _read_layout_snapshot(
    path: Path,
) -> Tuple[Dict[str, Any], Dict[str, Any]]:
    raw = _read_regular_file(
        path,
        label="布局快照",
        max_bytes=MAX_LAYOUT_SNAPSHOT_BYTES,
    )
    try:
        payload = json.loads(raw.decode("utf-8"))
    except (UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise ValueError("布局快照不是有效 JSON") from exc
    if not isinstance(payload, dict):
        raise ValueError("布局快照必须是 JSON 对象")
    if payload.get("schema_version") != LAYOUT_SNAPSHOT_SCHEMA_VERSION:
        raise ValueError("布局快照版本不受支持，请重新获取布局元素状态")

    layout_name = str(payload.get("layout_name") or "").strip()
    layout_item_ids = payload.get("layout_item_ids")
    layout_item_state = payload.get("layout_item_state")
    if not layout_name:
        raise ValueError("布局快照缺少 layout_name")
    if not isinstance(layout_item_ids, Mapping) or not layout_item_ids:
        raise ValueError("布局快照缺少 layout_item_ids")
    if not isinstance(layout_item_state, Mapping) or not layout_item_state:
        raise ValueError("布局快照缺少 layout_item_state")

    handed_off_ids = list(dict.fromkeys(_string_leaves(layout_item_ids)))
    if not handed_off_ids:
        raise ValueError("布局快照中没有可用的真实元素 ID")
    _validate_layout_item_state(
        layout_item_state,
        layout_name=layout_name,
        handed_off_ids=handed_off_ids,
    )

    state_items = layout_item_state.get("items")
    assert isinstance(state_items, Mapping)
    unregistered_ids = sorted(
        {str(item_id) for item_id in state_items} - set(handed_off_ids)
    )
    if unregistered_ids:
        raise ValueError(
            "布局快照包含未登记元素状态: " + ", ".join(unregistered_ids)
        )

    metadata = {
        "path": str(_absolute_path(path)),
        "sha256": hashlib.sha256(raw).hexdigest(),
        "byte_size": len(raw),
        "schema_version": LAYOUT_SNAPSHOT_SCHEMA_VERSION,
    }
    return payload, metadata


def _resolve_review_dpi(
    image_metadata: Mapping[str, Any],
    layout_item_state: Mapping[str, Any],
) -> Tuple[float, Dict[str, Any]]:
    page_size = layout_item_state.get("page_size_mm")
    if not isinstance(page_size, Mapping):
        raise ValueError("布局快照缺少 page_size_mm")
    page_width = _finite_number(page_size.get("width"), label="page_size_mm.width")
    page_height = _finite_number(page_size.get("height"), label="page_size_mm.height")
    if page_width <= 0 or page_height <= 0:
        raise ValueError("布局页面尺寸必须大于 0")

    pixel_width = int(image_metadata.get("width") or 0)
    pixel_height = int(image_metadata.get("height") or 0)
    if pixel_width <= 0 or pixel_height <= 0:
        raise ValueError("初版 PNG 像素尺寸无效")

    page_ratio = page_width / page_height
    pixel_ratio = pixel_width / pixel_height
    if abs(pixel_ratio - page_ratio) / page_ratio > 0.01:
        raise ValueError(
            "初版 PNG 的宽高比与布局快照页面不一致；"
            "请使用该布局重新导出审查图片"
        )

    derived_x = pixel_width * 25.4 / page_width
    derived_y = pixel_height * 25.4 / page_height
    actual_dpi = image_metadata.get("dpi")
    if isinstance(actual_dpi, Mapping):
        actual_x = _finite_number(actual_dpi.get("x"), label="PNG DPI.x")
        actual_y = _finite_number(actual_dpi.get("y"), label="PNG DPI.y")
        if actual_x <= 0 or actual_y <= 0:
            raise ValueError("初版 PNG 的 DPI 元数据无效")
        if (
            abs(actual_x - derived_x) / derived_x > 0.02
            or abs(actual_y - derived_y) / derived_y > 0.02
        ):
            raise ValueError(
                "初版 PNG 的像素尺寸、DPI 与布局快照页面不一致；"
                "请使用该布局重新导出审查图片"
            )
        resolved = (actual_x + actual_y) / 2.0
        source = "png_metadata"
    else:
        resolved = (derived_x + derived_y) / 2.0
        source = "derived_from_page_and_pixels"

    return round(resolved, 3), {
        "source": source,
        "x": round(derived_x, 3),
        "y": round(derived_y, 3),
    }


def _review_id() -> str:
    stamp = datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ")
    return f"{stamp}_{uuid.uuid4().hex[:8]}"


def _quality_artifact_paths(original_path: Path, review_id: str) -> Dict[str, Path]:
    root = original_path.parent
    prefix = f"{original_path.stem}_quality_{review_id}"
    return {
        "original": root / f"{prefix}_original.png",
        "final": root / f"{prefix}_final.png",
        "manifest": root / f"{prefix}_review.json",
    }


def _assert_distinct_paths(paths: Mapping[str, Path]) -> None:
    resolved: Dict[str, Path] = {}
    for role, raw_path in paths.items():
        path = _absolute_path(raw_path)
        if path.is_symlink():
            raise ValueError(f"质量反馈文件不能是符号链接: {role}")
        resolved[role] = path
    seen: Dict[Path, str] = {}
    for role, path in resolved.items():
        previous = seen.get(path)
        if previous:
            raise ValueError(f"质量反馈文件路径冲突: {previous} 与 {role}")
        seen[path] = role
    existing = [(role, path) for role, path in resolved.items() if path.exists()]
    for index, (left_role, left_path) in enumerate(existing):
        for right_role, right_path in existing[index + 1 :]:
            if os.path.samefile(left_path, right_path):
                raise ValueError(
                    f"质量反馈文件指向同一实体: {left_role} 与 {right_role}"
                )


def _write_new_file(path: Path, data: bytes) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    created = False
    try:
        with path.open("xb") as handle:
            created = True
            handle.write(data)
            handle.flush()
            os.fsync(handle.fileno())
    except Exception:
        if created:
            try:
                path.unlink()
            except FileNotFoundError:
                pass
        raise


def _preflight_artifact_paths(paths: Mapping[str, Path]) -> None:
    _assert_distinct_paths(paths)
    for role, path in paths.items():
        if path.exists() or path.is_symlink():
            raise FileExistsError(f"拒绝覆盖已有 {role} 文件: {path}")
        path.parent.mkdir(parents=True, exist_ok=True)
    probe = paths["manifest"].parent / f".quality-feedback-{uuid.uuid4().hex}.probe"
    try:
        _write_new_file(probe, b"")
    finally:
        try:
            probe.unlink()
        except FileNotFoundError:
            pass


def _atomic_write_json(path: Path, payload: Mapping[str, Any], *, create: bool) -> None:
    encoded = json.dumps(
        payload, ensure_ascii=False, indent=2, sort_keys=True
    ).encode("utf-8")
    if create:
        _write_new_file(path, encoded)
        return
    temp_path = path.with_name(f".{path.name}.{uuid.uuid4().hex}.tmp")
    try:
        _write_new_file(temp_path, encoded)
        os.replace(temp_path, path)
    finally:
        if temp_path.exists():
            temp_path.unlink()


def _artifact_record(
    *, role: str, path: Path, data: bytes, source_path: Path | None = None
) -> Dict[str, Any]:
    record = _inspect_image_bytes(data, "image/png")
    record.update({"role": role, "path": str(_absolute_path(path))})
    if source_path is not None:
        record["source_path"] = str(_absolute_path(source_path))
    return record


def _read_manifest(path: Path) -> Dict[str, Any]:
    if not path.is_absolute():
        raise ValueError("review_manifest_path 必须是绝对路径")
    try:
        payload = json.loads(
            _read_regular_file(path, label="质量反馈清单").decode("utf-8")
        )
    except Exception as exc:
        if isinstance(exc, (FileNotFoundError, ValueError)) and not isinstance(
            exc, (UnicodeDecodeError, json.JSONDecodeError)
        ):
            raise
        raise ValueError("质量反馈清单不是有效 JSON") from exc
    if not isinstance(payload, dict):
        raise ValueError("质量反馈清单格式不正确")
    if payload.get("schema_version") not in SUPPORTED_MANIFEST_SCHEMA_VERSIONS:
        raise ValueError("质量反馈清单版本不受支持，请重新执行初审准备")
    if not isinstance(payload.get("images"), dict):
        raise ValueError("质量反馈清单缺少 images")
    return payload


@contextmanager
def _manifest_lock(manifest_path: Path):
    """Serialize final-review binding for one manifest across CLI processes."""

    lock_path = manifest_path.with_name(f".{manifest_path.name}.lock")
    if lock_path.is_symlink():
        raise ValueError("质量反馈锁文件不能是符号链接")
    flags = os.O_CREAT | os.O_RDWR | getattr(os, "O_NOFOLLOW", 0)
    descriptor = os.open(lock_path, flags, 0o600)
    try:
        if not stat.S_ISREG(os.fstat(descriptor).st_mode):
            raise ValueError("质量反馈锁文件必须是普通文件")
        if os.name == "nt":
            import msvcrt

            if os.fstat(descriptor).st_size == 0:
                os.write(descriptor, b"\0")
            os.lseek(descriptor, 0, os.SEEK_SET)
            msvcrt.locking(descriptor, msvcrt.LK_LOCK, 1)
        else:
            import fcntl

            fcntl.flock(descriptor, fcntl.LOCK_EX)
        try:
            yield
        finally:
            if os.name == "nt":
                os.lseek(descriptor, 0, os.SEEK_SET)
                msvcrt.locking(descriptor, msvcrt.LK_UNLCK, 1)
            else:
                fcntl.flock(descriptor, fcntl.LOCK_UN)
    finally:
        os.close(descriptor)


def _verified_manifest_image(
    record: Any, *, expected_role: str
) -> Tuple[Path, Dict[str, Any]]:
    if not isinstance(record, Mapping) or record.get("role") != expected_role:
        raise ValueError(f"质量反馈清单缺少 {expected_role} 图像记录")
    path = _absolute_path(Path(str(record.get("path") or "")))
    data, mime_type = _detect_image(path)
    actual = _inspect_image_bytes(data, mime_type)
    if str(record.get("sha256") or "") != actual["sha256"]:
        raise ValueError(f"{expected_role} 图像已被覆盖或修改: {path}")
    for key in ("width", "height"):
        if int(record.get(key) or 0) != int(actual[key]):
            raise ValueError(f"{expected_role} 图像尺寸与清单不一致: {path}")
    actual.update({"role": expected_role, "path": str(path)})
    return path, actual


def prepare_review(params: Mapping[str, Any]) -> Dict[str, Any]:
    raw_original_path = str(params.get("original_image_path") or "").strip()
    raw_snapshot_path = str(params.get("layout_snapshot_path") or "").strip()
    task_requirement = str(params.get("task_requirement") or "").strip()
    locked_content = _string_list(params.get("locked_content"))

    if not raw_original_path:
        raise ValueError("缺少 original_image_path")
    if not raw_snapshot_path:
        raise ValueError("缺少 layout_snapshot_path")
    if not task_requirement:
        raise ValueError("缺少原始制图任务要求")
    if not locked_content:
        raise ValueError("锁定内容不能为空")

    original_path = _absolute_path(Path(raw_original_path))
    snapshot_path = _absolute_path(Path(raw_snapshot_path))
    snapshot, snapshot_metadata = _read_layout_snapshot(snapshot_path)
    layout_name = str(snapshot["layout_name"])
    layout_item_ids = snapshot["layout_item_ids"]
    layout_item_state = snapshot["layout_item_state"]

    original_data, original_mime = _detect_image(original_path)
    original_metadata = _inspect_image_bytes(original_data, original_mime)
    initial_export_dpi, derived_dpi = _resolve_review_dpi(
        original_metadata,
        layout_item_state,
    )
    review_id = _review_id()
    artifact_paths = _quality_artifact_paths(original_path, review_id)
    _preflight_artifact_paths(artifact_paths)

    created_paths: List[Path] = []
    try:
        _write_new_file(artifact_paths["original"], original_data)
        created_paths.append(artifact_paths["original"])
        original_record = _artifact_record(
            role="original",
            path=artifact_paths["original"],
            data=original_data,
            source_path=original_path,
        )
        review_context = {
            "layout_name": layout_name,
            "task_requirement": task_requirement,
            "locked_content": locked_content,
            "layout_item_ids": dict(layout_item_ids),
            "layout_item_state": dict(layout_item_state),
            "initial_export_dpi": initial_export_dpi,
            "derived_dpi": derived_dpi,
            "layout_snapshot": snapshot_metadata,
            "applied_suggestions": [],
        }
        manifest: Dict[str, Any] = {
            "schema_version": MANIFEST_SCHEMA_VERSION,
            "review_id": review_id,
            "status": "initial_ready",
            "created_at": datetime.now(timezone.utc).isoformat(),
            "review_context": review_context,
            "images": {
                "original": original_record,
                "final": {
                    "role": "final",
                    "path": str(_absolute_path(artifact_paths["final"])),
                    "metadata": None,
                },
            },
        }
        _atomic_write_json(artifact_paths["manifest"], manifest, create=True)
        created_paths.append(artifact_paths["manifest"])
    except Exception:
        for path in reversed(created_paths):
            try:
                path.unlink()
            except FileNotFoundError:
                pass
        raise

    protected_original = str(_absolute_path(artifact_paths["original"]))
    return {
        "stage": "initial_ready",
        "review_id": review_id,
        "review_manifest_path": str(_absolute_path(artifact_paths["manifest"])),
        "original_image_path": protected_original,
        "source_original_image_path": str(original_path),
        "layout_snapshot_path": str(snapshot_path),
        "reserved_final_image_path": str(_absolute_path(artifact_paths["final"])),
        "initial_export_dpi": initial_export_dpi,
        "original_image": original_record,
        "review_context": review_context,
        MEDIA_KEY: [
            {
                "type": "image",
                "path": protected_original,
                "label": "图1：受保护的 QGIS 初版地图（质量检查对象）",
            }
        ],
    }


def load_review_images(params: Mapping[str, Any]) -> Dict[str, Any]:
    raw_manifest_path = str(params.get("review_manifest_path") or "").strip()
    if not raw_manifest_path:
        raise ValueError("缺少 review_manifest_path")
    manifest_path = _absolute_path(
        Path(raw_manifest_path)
    )
    with _manifest_lock(manifest_path):
        return _load_review_images_locked(params, manifest_path=manifest_path)


def _load_review_images_locked(
    params: Mapping[str, Any], *, manifest_path: Path
) -> Dict[str, Any]:
    raw_final_path = str(params.get("final_image_path") or "").strip()
    if not raw_final_path:
        raise ValueError("缺少 final_image_path")
    final_path = _absolute_path(
        Path(raw_final_path)
    )
    applied_suggestions = params.get("applied_suggestions")
    if not isinstance(applied_suggestions, list) or any(
        not isinstance(item, Mapping) for item in applied_suggestions
    ):
        raise ValueError("applied_suggestions 必须是 JSON 对象数组，无修改时传空数组")

    manifest = _read_manifest(manifest_path)
    images = manifest["images"]
    review_context = manifest.get("review_context")
    if not isinstance(review_context, Mapping):
        raise ValueError("质量反馈清单缺少 review_context")
    original_path, original_metadata = _verified_manifest_image(
        images.get("original"), expected_role="original"
    )

    final_record = images.get("final")
    if not isinstance(final_record, Mapping) or final_record.get("role") != "final":
        raise ValueError("质量反馈清单缺少 final 图像记录")
    reserved_final = _absolute_path(Path(str(final_record.get("path") or "")))
    if final_path != reserved_final:
        raise ValueError(f"终稿必须导出到质量反馈预留路径: {reserved_final}")

    final_data, final_mime = _detect_image(final_path)
    final_metadata = _inspect_image_bytes(final_data, final_mime)
    _assert_distinct_paths({"original": original_path, "final": final_path})
    if (
        int(final_metadata["width"]) != int(original_metadata["width"])
        or int(final_metadata["height"]) != int(original_metadata["height"])
    ):
        raise ValueError(
            "QGIS 终稿必须保持初版像素尺寸: "
            f"初版 {original_metadata['width']}x{original_metadata['height']}，"
            f"终稿 {final_metadata['width']}x{final_metadata['height']}"
        )
    original_dpi = original_metadata.get("dpi")
    final_dpi = final_metadata.get("dpi")
    if isinstance(original_dpi, Mapping):
        if not isinstance(final_dpi, Mapping) or any(
            abs(float(original_dpi[axis]) - float(final_dpi[axis])) > 0.5
            for axis in ("x", "y")
        ):
            raise ValueError("QGIS 终稿必须保持初版 DPI")

    final_metadata.update({"role": "final", "path": str(final_path)})
    existing_final = final_record.get("metadata")
    if isinstance(existing_final, Mapping):
        if existing_final.get("sha256") != final_metadata["sha256"]:
            raise ValueError("该质量反馈清单已绑定另一份终稿")
        if review_context.get("applied_suggestions") != applied_suggestions:
            raise ValueError("该质量反馈清单已绑定另一组已执行建议")
    else:
        manifest["images"]["final"] = {
            "role": "final",
            "path": str(reserved_final),
            "metadata": final_metadata,
        }
        manifest["review_context"]["applied_suggestions"] = [
            dict(item) for item in applied_suggestions
        ]
        manifest["status"] = "final_ready"
        manifest["updated_at"] = datetime.now(timezone.utc).isoformat()
        _atomic_write_json(manifest_path, manifest, create=False)

    return {
        "review_id": manifest.get("review_id"),
        "review_manifest_path": str(manifest_path),
        "original_image_path": str(original_path),
        "final_image_path": str(final_path),
        "original_image": original_metadata,
        "final_image": final_metadata,
        "review_context": dict(manifest["review_context"]),
        MEDIA_KEY: [
            {
                "type": "image",
                "path": str(original_path),
                "label": "图1：受保护的 QGIS 初版地图（内容基准）",
            },
            {
                "type": "image",
                "path": str(final_path),
                "label": "图2：QGIS 修改后终稿（待复检）",
            },
        ],
    }


def _read_params() -> Dict[str, Any]:
    raw = sys.stdin.buffer.read()
    if not raw.strip():
        return {}
    payload = json.loads(raw.decode("utf-8"))
    if not isinstance(payload, dict):
        raise ValueError("工具输入必须是 JSON object")
    return payload


def _emit(payload: Mapping[str, Any]) -> None:
    sys.stdout.write(json.dumps(payload, ensure_ascii=False, separators=(",", ":")))
    sys.stdout.flush()


def main(argv: List[str]) -> int:
    mode = argv[1] if len(argv) > 1 else "prepare"
    try:
        params = _read_params()
        if mode == "prepare":
            output = prepare_review(params)
        elif mode == "load-review":
            output = load_review_images(params)
        else:
            raise ValueError(f"未知模式: {mode}")
        _emit({"success": True, "output": output, "error": "", "output_layers": []})
    except Exception as exc:
        _emit(
            {
                "success": False,
                "output": None,
                "error": str(exc),
                "output_layers": [],
            }
        )
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv))
