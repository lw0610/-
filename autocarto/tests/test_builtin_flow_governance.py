"""Flows are free business prose; only metadata and visual references are read."""
from pathlib import Path
import re
import pytest

ROOT = Path(__file__).resolve().parents[2]
FLOWS = ROOT / "autocarto/flows/system"

@pytest.mark.parametrize("flow_id", ["marching-route", "statistical-choropleth", "traffic-route", "data-to-map"])
def test_builtin_flows_do_not_restore_the_old_executable_contract(flow_id):
    raw = (FLOWS / flow_id / "FLOW.md").read_text(encoding="utf-8")
    assert not re.search(r"(?m)^## (参与角色|数据需求|需要准备|使用的资源)$", raw)
    assert not re.search(r"(?m)^- (负责|执行者|需要能力|产出要求|失败时)：", raw)
    assert not (FLOWS / flow_id / "resources").exists()
    assert not re.search(r"(?m)^\s*(?:input_path|output_path|layer_id)\s*[:=]", raw)
    # Natural numbered instructions and tables are valid prose, not a DSL.
    assert len(raw) > 1000


def test_route_flow_matches_v6_normative_example():
    spec = (ROOT / "docs/refactor-v6/05-行军路线图流程示例.md").read_text()
    expected = spec.split("````markdown\n", 1)[1].split("\n````", 1)[0]
    actual = (FLOWS / "marching-route/FLOW.md").read_text()
    assert actual.strip() == expected.strip()
