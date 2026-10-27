"""G2 keeps business workflow structure while limiting tool manual prose."""
from pathlib import Path
import re
import json
import hashlib

import pytest

ROOT = Path(__file__).resolve().parents[2]
# UTF-8 business-prose sizes at R30 checkpoint a22a1a8, using the same projection
# below for both baseline and current files. G2 governs instruction/manual prose;
# V3's required typed input and resource declarations are a separate contract.
BASELINE_BYTES = {"marching-route": 13872, "statistical-choropleth": 6865}


def business_prose(body):
    lines = []
    include = True
    for line in body.splitlines(keepends=True):
        heading = re.match(r"^## (.+?)\s*$", line)
        if heading:
            include = heading.group(1) not in {"需要准备", "使用的资源", "参与角色", "数据需求"}
        if include:
            lines.append(line)
    return "".join(lines)


@pytest.mark.parametrize("flow_id", BASELINE_BYTES)
def test_builtin_business_prose_is_concise(flow_id):
    raw = (ROOT / "autocarto/flows/system" / flow_id / "FLOW.md").read_text(encoding="utf-8")
    body = raw.split("---", 2)[2]
    assert len(business_prose(body).encode("utf-8")) <= BASELINE_BYTES[flow_id] * .7
    assert len(re.findall(r"不要|不能|必须|禁止|不得|只能", body)) <= 8


def test_business_prose_excludes_only_declaration_sections():
    before = "## 适用场景\n真实材料制图\n"
    declarations = (
        "## 需要准备\n- 底图（必填，文件夹）：包含原始图层\n"
        "## 使用的资源\n- 数据清单「底图」：resources/data/base.csv（文件所在文件夹：「底图」）\n"
        "## 参与角色\n### 数据工程专家\n- 工具包：数据加载与导出\n"
        "## 数据需求\n### 底图\n- 清单：「底图」\n- 范围：全国\n"
    )
    after = (
        "## 步骤\n### 加载数据\n- 做法：保留真实来源并校验文件。\n"
        "## 必须遵守\n- 不伪造数据。\n## 交付\n- 成果路径\n"
        "## 额外说明\n未知章节仍受正文体积约束。\n"
    )
    assert business_prose(before + declarations + after) == before + after


def test_marching_business_steps_preserve_v3_semantics():
    baseline = json.loads((ROOT / "autocarto/tests/fixtures/builtin_marching_steps_before_v4.json").read_text())
    raw = (ROOT / "autocarto/flows/system/marching-route/FLOW.md").read_text()
    steps = dict(re.findall(r"(?m)^### (第\d+步 [^\n]+)\n(.*?)(?=^### |^## |\Z)", raw, re.S))
    assert steps.keys() == baseline["steps"].keys()
    for title, body in steps.items():
        if title.startswith("第5步 "):
            assert "「数据来源说明」" in body
            continue
        assert hashlib.sha256(body.strip().encode()).hexdigest() == baseline["steps"][title], title
