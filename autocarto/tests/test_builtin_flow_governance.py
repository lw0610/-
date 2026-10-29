"""Flows are free business prose; only metadata and visual references are read."""
from pathlib import Path
import re
import pytest

ROOT = Path(__file__).resolve().parents[2]
FLOWS = ROOT / "autocarto/flows/system"

@pytest.mark.parametrize("flow_id", ["marching-route", "statistical-choropleth"])
def test_builtin_flows_do_not_restore_the_old_executable_contract(flow_id):
    raw = (FLOWS / flow_id / "FLOW.md").read_text(encoding="utf-8")
    assert "## 参与角色" not in raw
    assert not re.search(r"(?m)^- (负责|执行者|需要能力|产出要求|失败时)：", raw)
    assert not (FLOWS / flow_id / "resources").exists()
    assert not re.search(r"(?m)^\s*(?:input_path|output_path|layer_id)\s*[:=]", raw)
    # Natural numbered instructions and tables are valid prose, not a DSL.
    assert len(raw) > 1000


def test_route_flow_preserves_knowledge_instructions_and_confirmation():
    actual = (FLOWS / "marching-route/FLOW.md").read_text()
    assert "多主体可属于同一条多路进攻路线" in actual
    assert "用户确认后再制图" in actual


def test_route_flow_keeps_its_visual_resource_references():
    raw = (FLOWS / "marching-route/FLOW.md").read_text()
    assert set(re.findall(r"样式「([^」]+)」", raw)) == {"铁路"}
    assert "版面「四角对齐版式」" in raw
