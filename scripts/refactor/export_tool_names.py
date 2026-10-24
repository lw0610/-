#!/usr/bin/env python3
"""Export canonical tool metadata as a UTF-8 CSV for editorial review."""

import argparse
import csv
import json
from pathlib import Path
import sys

try:
    import yaml
except ImportError as error:
    raise SystemExit("导出工具元数据需要 PyYAML；可使用已包含该库的 QGIS Python。") from error


REPO_ROOT = Path(__file__).resolve().parents[2]
FIELDNAMES = (
    "name", "display_name", "description", "directory", "domain", "keywords", "file"
)


def load_tool_rows(repo_root):
    rows = []
    for path in sorted((repo_root / "autocarto/tools/system").rglob("*.yaml")):
        definition = yaml.safe_load(path.read_text(encoding="utf-8-sig"))
        if not isinstance(definition, dict) or definition.get("kind") not in {"Tool", "ToolPackage"}:
            continue
        metadata = definition.get("metadata", {})
        spec = definition.get("spec", {})
        relative = path.relative_to(repo_root)
        rows.append({
            "name": metadata.get("name", ""),
            "display_name": metadata.get("display_name", ""),
            "description": spec.get("description", ""),
            "directory": relative.parent.as_posix(),
            "domain": metadata.get("domain", ""),
            "keywords": json.dumps(metadata.get("keywords", []), ensure_ascii=False),
            "file": relative.as_posix(),
        })
    return rows


def write_csv(rows, stream):
    writer = csv.DictWriter(stream, fieldnames=FIELDNAMES)
    writer.writeheader()
    writer.writerows(rows)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=REPO_ROOT, help="项目根目录")
    parser.add_argument("--output", type=Path, help="CSV 输出文件；省略时写入标准输出")
    args = parser.parse_args()
    rows = load_tool_rows(args.root.resolve())
    if args.output:
        with args.output.open("w", encoding="utf-8-sig", newline="") as stream:
            write_csv(rows, stream)
    else:
        write_csv(rows, sys.stdout)


if __name__ == "__main__":
    main()
