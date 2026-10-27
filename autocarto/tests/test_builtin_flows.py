"""内置流程内容通过真实 Go 解析、编译及 HTTP 契约验收。"""
from __future__ import annotations

import csv
import io
import json
import os
from pathlib import Path
import re
import shutil
import subprocess

import pytest
import yaml


ROOT = Path(__file__).resolve().parents[2]
FLOWS = ROOT / "autocarto" / "flows" / "system"
EXPECTED_STEPS = {"marching-route": 11, "statistical-choropleth": 4, "traffic-route": 4, "data-to-map": 6}


@pytest.fixture(scope="module")
def backend_results(tmp_path_factory):
    executable = shutil.which("go")
    assert executable, "需要 Go 工具链执行真实流程解析与编译，不能以文本模拟替代"
    runtime = tmp_path_factory.mktemp("builtin-flow-validation")
    env = os.environ.copy()
    env.update({
        "AUTOCARTO_PROJECT_ROOT": str(ROOT),
        "AUTOCARTO_ASSETS_ROOT": str(ROOT / "autocarto"),
        "AUTOCARTO_HOME": str(runtime),
        "GOPROXY": "off",
        "AUTOCARTO_DISABLE_KEYRING": "1",
    })
    result = subprocess.run(
        [executable, "test", "./internal/api", "-run", "^TestBuiltinFlowsParseAndCompile$", "-count=1", "-v"],
        cwd=ROOT / "carto_agent", env=env, text=True, capture_output=True, timeout=180,
    )
    assert result.returncode == 0, result.stdout + result.stderr
    markers = [line.split("BUILTIN_FLOW_RESULT=", 1)[1] for line in result.stdout.splitlines() if "BUILTIN_FLOW_RESULT=" in line]
    assert len(markers) == 1, result.stdout
    return json.loads(markers[0])


def test_builtin_flows_use_real_parser_compiler_and_both_scheme_contracts(backend_results):
    expected = {(scheme, flow_id) for scheme in ("builtin-v1", "builtin-v2", "builtin-current", "builtin-zttzt") for flow_id in EXPECTED_STEPS}
    assert {(result["scheme"], result["flow_id"]) for result in backend_results} == expected
    for result in backend_results:
        assert result["steps"] == EXPECTED_STEPS[result["flow_id"]]
        assert not [diagnostic for diagnostic in result["diagnostics"] if diagnostic["level"] == "error"]
        assert not [diagnostic for diagnostic in result["diagnostics"] if diagnostic["code"] in {
            "FLOW_COMPILE_LLM_FAILED", "FLOW_CONDITION_UNPARSED", "FLOW_REF_UNRESOLVED", "FLOW_CAPABILITY_MISSING_IN_ROLE",
        }]


def test_exactly_four_builtin_flows_have_portable_business_instructions():
    assert {path.parent.name for path in FLOWS.glob("*/FLOW.md")} == set(EXPECTED_STEPS)
    for flow_id in EXPECTED_STEPS:
        raw = (FLOWS / flow_id / "FLOW.md").read_text(encoding="utf-8")
        front = yaml.safe_load(raw.split("---", 2)[1])
        assert front["编号"] == flow_id
        assert front["名称"]
        assert front["版本"] == "3.0.0"
        assert "## 参与角色" in raw
        assert not re.search(r"/Users/|[A-Za-z]:[\\/]|\.\.\\|autocarto_data", raw), flow_id


def test_flow_bodies_do_not_embed_tool_english_names():
    names = set()
    for path in (ROOT / "autocarto" / "tools").rglob("*.yaml"):
        definition = yaml.safe_load(path.read_text(encoding="utf-8-sig"))
        if isinstance(definition, dict) and isinstance(definition.get("metadata"), dict):
            name = definition["metadata"].get("name")
            if name:
                names.add(name)
    assert len(names) > 300
    for path in FLOWS.glob("*/FLOW.md"):
        raw = path.read_text(encoding="utf-8")
        leaked = sorted(name for name in names if re.search(r"(?<![A-Za-z0-9_])" + re.escape(name) + r"(?![A-Za-z0-9_])", raw))
        assert not leaked, (path.parent.name, leaked)


def test_route_basemap_has_eight_required_relative_original_sources():
    path = FLOWS / "marching-route" / "resources" / "data" / "中国底图.csv"
    rows = list(csv.DictReader(io.StringIO(path.read_text(encoding="utf-8-sig"))))
    expected = {"省域", "县级城市驻地", "铁路", "公路", "国道", "地区界线", "河流线", "河流面"}
    assert len(rows) == 8
    assert {row["名称"] for row in rows} == expected
    for row in rows:
        assert row["路径"] == row["名称"] + ".shp"
        assert row["类型"] == "矢量"
        assert row["必需"] == "是"
        assert row["类别"]
        assert row["字段"] == {"省域": "NAME", "县级城市驻地": "name", "河流线": "NAME"}.get(row["名称"], "")
        assert not Path(row["路径"]).is_absolute()
        assert ".." not in Path(row["路径"]).parts


def test_declared_choice_defaults_belong_to_options():
    declarations = 0
    for path in FLOWS.glob("*/FLOW.md"):
        for match in re.finditer(r"^- .+?（([^）]+)）", path.read_text(encoding="utf-8"), re.MULTILINE):
            attributes = re.split(r"[；;，,]", match[1])
            defaults = [item.removeprefix("默认").strip() for item in attributes if item.startswith("默认")]
            options = [item.split("：", 1)[1].strip().split("、") for item in attributes if item.startswith("可选值：")]
            if defaults and options:
                declarations += 1
                assert defaults[0] in options[0], (path.parent.name, defaults, options)
    assert declarations >= 5
