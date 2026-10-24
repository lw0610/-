"""声明契约在此解析验证；实际尺寸、主图、CRS 和网格由独立 QGIS 场景验证。"""
import unittest
from pathlib import Path
import yaml


class ScaleBarLayoutContractTestCase(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.workspace = Path(__file__).resolve().parents[2]

    def test_reference_alignment_is_exposed_to_layout_agent(self):
        tool = yaml.safe_load((self.workspace/"autocarto/tools/system/print-layout/layout_align_items.yaml").read_text())
        parameters = {item["name"]: item for item in tool["spec"]["parameters"]}
        self.assertEqual(parameters["reference_item_id"]["type"], "string")
        self.assertEqual(tool["execution"]["param_mapping"]["reference_item_id"], "{{ reference_item_id }}")
        raw = (self.workspace/"autocarto/skills/system/print-layout/SKILL.md").read_text()
        self.assertIn("layout_align_items", yaml.safe_load(raw.split("---", 2)[1])["tools"])

    def test_builtin_scalebar_uses_reference_anchor_and_width_budget(self):
        spec = yaml.safe_load((self.workspace/"autocarto/resources/system/layouts/四角对齐版式.layout.yaml").read_text())
        scalebar = spec["elements"]["scalebar"]
        self.assertEqual(scalebar["anchor"], {"to": "main_map", "at": "bottom_right", "placement": "inside"})
        self.assertEqual(scalebar["max_width_mm"], 80)
        self.assertNotIn("width_mm", scalebar)


if __name__ == "__main__":
    unittest.main()
