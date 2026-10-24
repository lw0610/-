"""Compare structural snapshots; preview differences are diagnostic only."""
from __future__ import annotations

import argparse
import fnmatch
import json
import math
from pathlib import Path
from typing import Any


def _ignored(path: str, patterns: list[str]) -> bool:
    return any(fnmatch.fnmatchcase(path, pattern) for pattern in patterns)


def compare_snapshots(expected: dict, actual: dict, *, tolerance_mm: float = 1.0,
                      ignore: list[str] | None = None) -> list[dict[str, Any]]:
    """Compare every captured invariant except explicitly non-comparable metadata.

    Ignore patterns use slash-separated paths, e.g. layers/*/renderer_sha256.
    Coordinate tolerance applies only to item rectangles, not fonts or hashes.
    """
    if not math.isfinite(tolerance_mm) or tolerance_mm < 0:
        raise ValueError("矩形容差必须为非负有限数值")
    patterns = list(ignore or []) + ["metadata/qgis_version", "metadata/external_inputs", "driver", "preview"]
    differences = []

    def walk(left: Any, right: Any, path: str):
        if _ignored(path, patterns):
            return
        if isinstance(left, dict) and isinstance(right, dict):
            for key in sorted(set(left) | set(right)):
                child = f"{path}/{key}" if path else key
                if _ignored(child, patterns):
                    continue
                if key not in left or key not in right:
                    differences.append({"path": child, "expected": left.get(key), "actual": right.get(key), "reason": "字段或元素集合不同"})
                else:
                    walk(left[key], right[key], child)
        elif isinstance(left, (int, float)) and not isinstance(left, bool) and isinstance(right, (int, float)) and not isinstance(right, bool):
            tolerance = tolerance_mm if path.split("/")[-2] in {"rect_mm", "outer_rect_mm"} else 1e-6
            if not math.isfinite(left) or not math.isfinite(right) or abs(left - right) > tolerance:
                differences.append({"path": path, "expected": left, "actual": right, "tolerance": tolerance})
        elif left != right:
            differences.append({"path": path, "expected": left, "actual": right})

    walk(expected, actual, "")
    return differences


def perceptual_hash(path: Path) -> str | None:
    """64-bit difference hash; image changes never determine structural success."""
    try:
        from PIL import Image
    except ImportError:
        return None
    with Image.open(path) as image:
        small = image.convert("L").resize((9, 8))
        read_pixels = getattr(small, "get_flattened_data", small.getdata)
        pixels = list(read_pixels())
    value = 0
    for row in range(8):
        for col in range(8):
            value = (value << 1) | int(pixels[row * 9 + col] > pixels[row * 9 + col + 1])
    return f"{value:016x}"


def compare_directories(baseline: Path, candidate: Path, *, tolerance_mm=1.0,
                        ignore=None) -> dict:
    expected = json.loads((baseline / "snapshot.json").read_text(encoding="utf-8"))
    actual = json.loads((candidate / "snapshot.json").read_text(encoding="utf-8"))
    differences = compare_snapshots(expected, actual, tolerance_mm=tolerance_mm, ignore=ignore)
    hashes = [perceptual_hash(directory / "preview.png") if (directory / "preview.png").is_file() else None for directory in (baseline, candidate)]
    distance = (int(hashes[0], 16) ^ int(hashes[1], 16)).bit_count() if all(hashes) else None
    return {"success": not differences, "differences": differences,
            "preview": {"algorithm": "dhash64", "distance": distance, "diagnostic_only": True}}


def main(argv=None) -> int:
    parser = argparse.ArgumentParser(description="对比黄金样例的版面、图层和样式快照")
    parser.add_argument("baseline", type=Path)
    parser.add_argument("candidate", type=Path)
    parser.add_argument("--tolerance-mm", type=float, default=1.0)
    parser.add_argument("--config", type=Path, help="JSON 对象：ignore 路径模式数组及可选 tolerance_mm")
    args = parser.parse_args(argv)
    config = json.loads(args.config.read_text(encoding="utf-8")) if args.config else {}
    result = compare_directories(args.baseline, args.candidate, tolerance_mm=config.get("tolerance_mm", args.tolerance_mm), ignore=config.get("ignore", []))
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return 0 if result["success"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
