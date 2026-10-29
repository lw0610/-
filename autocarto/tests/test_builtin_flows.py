"""内置指南内容通过真实 Go 解析、资源及 HTTP 契约验收。"""
from __future__ import annotations

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
EXPECTED_GUIDES = {"marching-route", "statistical-choropleth"}


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
        [executable, "test", "./internal/api", "-run", "^TestBuiltinGuidesParseAndResolve$", "-count=1", "-v"],
        cwd=ROOT / "carto_agent", env=env, text=True, capture_output=True, timeout=180,
    )
    assert result.returncode == 0, result.stdout + result.stderr
    markers = [line.split("BUILTIN_GUIDE_RESULT=", 1)[1] for line in result.stdout.splitlines() if "BUILTIN_GUIDE_RESULT=" in line]
    assert len(markers) == 1, result.stdout
    return json.loads(markers[0])


def test_builtin_guides_use_real_parser_resources_and_http(backend_results):
    assert {result["id"] for result in backend_results} == EXPECTED_GUIDES
    for result in backend_results:
        assert result["references"] > 0
        assert result["body_bytes"] > 1000
        assert not [d for d in result["diagnostics"] if d["level"] in {"error", "warning"}]


def test_builtin_flows_have_portable_business_instructions():
    assert {path.parent.name for path in FLOWS.glob("*/FLOW.md")} == set(EXPECTED_GUIDES)
    for flow_id in EXPECTED_GUIDES:
        raw = (FLOWS / flow_id / "FLOW.md").read_text(encoding="utf-8")
        front = yaml.safe_load(raw.split("---", 2)[1])
        assert front.get("编号", flow_id) == flow_id
        assert front["名称"]
        assert front["版本"] == "6.0.0"
        assert front["说明"]
        assert "## 参与角色" not in raw
        assert not re.search(r"(?m)^- 负责：", raw)
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


def test_flow_packages_contain_only_document_and_visual_dependency_lock():
    for flow_id in EXPECTED_GUIDES:
        directory = FLOWS / flow_id
        assert not (directory / "resources").exists()
        assert not (directory / "GUIDE.md").exists()
        lock = yaml.safe_load((directory / "flow.lock").read_text())
        assert lock["resources"]
        assert all(row["type"] in {"style", "layout"} for row in lock["resources"])


def test_route_basemap_needs_are_explained_in_free_document():
    raw = (FLOWS / "marching-route" / "FLOW.md").read_text()
    for name in {"省域", "市级驻地", "铁路", "公路", "国道", "地区界线", "河流线", "河流面", "地形晕渲", "鄂豫皖革命根据地", "鄂豫陕革命根据地", "陕甘宁革命根据地"}:
        assert name in raw
    assert "resources/data/" not in raw
    assert "清单「" not in raw
