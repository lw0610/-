#!/usr/bin/env python3
"""Run normal tests together and each QGIS module in its own application process."""
import os
from pathlib import Path
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[2]
env = os.environ.copy()
env.setdefault("QT_QPA_PLATFORM", "offscreen")
# Bundled GIS databases must belong to the same runtime as its native libraries.
for parent in Path(sys.executable).resolve().parents:
    data = parent / "Resources/qgis"
    if data.is_dir():
        env.setdefault("PROJ_DATA", str(data / "proj"))
        env.setdefault("GDAL_DATA", str(data / "gdal"))
        env["PYTHONPATH"] = str(data / "python/plugins") + os.pathsep + env.get("PYTHONPATH", "")
        break
qgis_modules = sorted((ROOT / "autocarto/tests").rglob("test_*_qgis.py"))
commands = [[sys.executable, "-m", "pytest", "autocarto/tests", "--ignore-glob=*_qgis.py", "-q", "-ra"]]
commands += [[sys.executable, "-m", "pytest", str(path.relative_to(ROOT)), "-q", "-ra"] for path in qgis_modules]
failed = []
for command in commands:
    label = "普通测试" if "--ignore-glob=*_qgis.py" in command else command[3]
    print("\nPython 检查：" + label, flush=True)
    result = subprocess.run(command, cwd=ROOT, env=env)
    if result.returncode:
        failed.append((label, result.returncode))
if failed:
    for label, code in failed:
        print(f"未通过：{label}（退出码 {code}）", flush=True)
    raise SystemExit(1)
print(f"Python 检查通过：普通测试与 {len(qgis_modules)} 个独立 QGIS 测试模块。", flush=True)
