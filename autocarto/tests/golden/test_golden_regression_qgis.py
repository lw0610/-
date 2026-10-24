"""Real subprocess regressions; skips are explicit when prerequisites are absent."""
import json
import os
from pathlib import Path
import subprocess
import sys

import pytest

from .compare_golden import compare_directories
from .qgis_bootstrap import qgis_available
from .run_golden import NEW_DRIVER_READY, external_route_inputs, GoldenUnavailable


@pytest.mark.parametrize("scenario", ["route", "statistical"])
@pytest.mark.parametrize("driver", ["legacy", "new"])
def test_fixed_input_matches_committed_baseline(scenario, driver, tmp_path):
    if not qgis_available():
        pytest.skip("QGIS Python 不可用；请在独立 QGIS Python 环境执行")
    if driver == "new" and not NEW_DRIVER_READY:
        pytest.skip("新驱动待 T41/T43/T44 完成；不以旧驱动冒充新实现")
    if scenario == "route":
        try:
            external_route_inputs()
        except GoldenUnavailable as exc:
            pytest.skip(str(exc))
    baseline = Path(__file__).parent / "baseline" / scenario
    assert (baseline / "snapshot.json").is_file(), f"缺少真实基线: {baseline}"
    output = tmp_path / scenario
    environment = dict(os.environ, QT_QPA_PLATFORM="offscreen", PYTHONDONTWRITEBYTECODE="1")
    process = subprocess.run([sys.executable, str(Path(__file__).with_name("run_golden.py")),
                              "--scenario", scenario, "--driver", driver, "--out", str(output)],
                             env=environment, capture_output=True, text=True, timeout=240)
    if process.returncode == 77:
        pytest.skip(process.stdout.strip())
    assert process.returncode == 0, process.stdout + "\n" + process.stderr
    # T01 旧驱动遗漏加粗；当前实际字重变化仍在原几何容差内，全量比较。
    result = compare_directories(baseline, output)
    assert result["success"], json.dumps(result, ensure_ascii=False, indent=2)
    if driver == "new":
        title = json.loads((output/"compose_validation.json").read_text())
        assert title["title_bold"] and title["title_font_locked"]
        assert title["title_anchor_error_mm"] <= 1
