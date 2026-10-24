"""运行真实 Controller/QGIS 离线验收，成果保留在调用者指定的新目录。"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[4]


def expand(value, variables):
    if isinstance(value, str):
        for name, replacement in variables.items():
            value = value.replace("${"+name+"}", str(replacement))
        return value
    if isinstance(value, dict):
        return {key: expand(child, variables) for key, child in value.items()}
    if isinstance(value, list):
        return [expand(child, variables) for child in value]
    return value



def input_hashes():
    result = {}
    roots = [ROOT/"autocarto/tests/golden/fixtures", ROOT/"docs/refactor/acceptance-flows"]
    basemap = os.environ.get("AUTOCARTO_GOLDEN_BASEMAP_DIR")
    if basemap:
        roots.append(Path(basemap))
    for root in roots:
        for path in root.rglob("*"):
            if path.is_file() and not path.is_symlink() and "__pycache__" not in path.parts:
                result[str(path)] = hashlib.sha256(path.read_bytes()).hexdigest()
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("recipes", nargs="+", type=Path)
    parser.add_argument("--out", required=True, type=Path)
    parser.add_argument("--python", default=sys.executable)
    args = parser.parse_args()
    args.out = args.out.resolve()
    args.out.mkdir(parents=True, exist_ok=False)
    environment = dict(os.environ, AUTOCARTO_PROJECT_ROOT=str(ROOT), AUTOCARTO_ASSETS_ROOT=str(ROOT/"autocarto"),
                       AUTOCARTO_HOME=str(args.out/"build-runtime"), PYTHONDONTWRITEBYTECODE="1", QT_QPA_PLATFORM="offscreen", GOPROXY="off")
    executable = args.out/"acceptance-runner"
    subprocess.run(["go", "build", "-o", str(executable), "./integration/acceptance"], cwd=ROOT/"carto_agent", env=environment, check=True)
    results = []
    for path in args.recipes:
        path = path.resolve()
        scenario = json.loads(path.read_text())
        output = args.out/scenario["id"]
        output.mkdir()
        variables = {"root": ROOT, "assets": ROOT/"autocarto", "fixture": ROOT/"autocarto/tests/golden/fixtures",
                     "out": output, "package": path.parent, "basemap": os.environ.get("AUTOCARTO_GOLDEN_BASEMAP_DIR", ""),
                     "renderer": os.environ.get("AUTOCARTO_ROUTE_RENDERER_DIR", "")}
        scenario = expand(scenario, variables)
        scenario.update(root=str(ROOT), out=str(output), python=args.python, examples=str(ROOT/"docs/refactor/acceptance-flows"))
        scenario["variables"].update({key: str(value) for key, value in variables.items()})
        config = output/"scenario.json"
        config.write_text(json.dumps(scenario, ensure_ascii=False, indent=2)+"\n")
        before = input_hashes()
        (output/"input-hashes-before.json").write_text(json.dumps(before, ensure_ascii=False, indent=2)+"\n")
        process = subprocess.run([str(executable), str(config)], cwd=ROOT/"carto_agent", env=environment, text=True, capture_output=True)
        (output/"controller.log").write_text(process.stdout+process.stderr)
        after = input_hashes()
        (output/"input-hashes-after.json").write_text(json.dumps(after, ensure_ascii=False, indent=2)+"\n")
        if before != after:
            raise AssertionError("验收输入数据被修改，拒绝记录通过")
        result = {"inputs_unchanged": True, "scenario": scenario["id"], "success": process.returncode == 0, "output": str(output), "returncode": process.returncode}
        results.append(result)
        print(json.dumps(result, ensure_ascii=False), flush=True)
        if process.returncode:
            print(process.stdout[-500:]+process.stderr[-3000:], file=sys.stderr)
    (args.out/"results.json").write_text(json.dumps(results, ensure_ascii=False, indent=2)+"\n")
    return 0 if all(result["success"] for result in results) else 1


if __name__ == "__main__":
    raise SystemExit(main())
