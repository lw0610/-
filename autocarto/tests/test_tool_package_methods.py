"""Migration keeps methods with tool ownership and preserves licensed GEE assets."""
from pathlib import Path
import hashlib
import json
import re
import unittest

import yaml

ROOT = Path(__file__).resolve().parents[1] / "skills" / "system"
REFERENCE_METHODS = {
    "accessibility-service-area": ("network-analysis", "可达性与服务区"),
    "attribute-cleaning": ("vector-fields", "属性清洗"),
    "buffer-proximity-analysis": ("vector-geometry", "缓冲与邻近分析"),
    "conditional-feature-extraction": ("vector-selection", "条件提取"),
    "data-format-conversion": ("data-conversion", "多格式互转"),
    "dem-terrain-derivatives": ("raster-analysis", "地形衍生"),
    "geometry-repair-validation": ("vector-geometry", "几何修复"),
    "interpolation-surface": ("raster-analysis", "空间插值"),
    "land-use-change-detection": ("raster-analysis", "土地利用变化检测"),
    "map-series-batch-export": ("print-layout", "批量出图"),
    "overlay-analysis": ("vector-geometry", "叠加分析"),
    "raster-algebra": ("raster-analysis", "栅格代数"),
    "raster-reclassify-stats": ("raster-analysis", "重分类与分区统计"),
    "sampling-random-selection": ("vector-selection", "随机抽样"),
    "site-suitability-analysis": ("raster-analysis", "选址适宜性分析"),
    "watershed-hydrology-analysis": ("grass-analysis", "流域水文分析"),
}
INLINE_METHODS = {
    "chart-visualization": "charts", "crs-diagnosis-transform": "crs-projection",
    "data-loading-inspection": "data-loading", "gps-field-data-import": "gps-data",
    "mesh-3d-analysis": "mesh-analysis", "network-routing-analysis": "network-analysis",
    "point-cloud-processing": "point-cloud", "spatial-clustering": "spatial-analysis",
    "statistical-data-preparation": "statistical-data-tools", "thematic-map-styling": "cartography-styling",
    "viewshed-visibility-analysis": "visibility-analysis", "gee-scripting": "gee-remote-sensing",
}
PYTHON_METHODS = "attribute-ops batch-processing crs-transform data-loading geometry-ops interpolation layout raster-analysis spatial-query styling terrain-ops vector-analysis workflow".split()
for _suffix in PYTHON_METHODS:
    _source = "pyqgis-" + _suffix
    REFERENCE_METHODS[_source] = ("pyqgis-scripting", _source)


def package(name):
    raw = (ROOT / name / "SKILL.md").read_text(encoding="utf-8")
    _, front, body = raw.split("---", 2)
    return yaml.safe_load(front), body


class TestToolPackageMethods(unittest.TestCase):
    def test_retired_method_roots_are_gone_and_target_has_substantial_body(self):
        for source, target in {**INLINE_METHODS, **{k: v[0] for k, v in REFERENCE_METHODS.items()}}.items():
            with self.subTest(source=source):
                self.assertFalse((ROOT / source).exists())
                target_front, body = package(target)
                self.assertEqual(target_front["kind"], "category")
                content = "".join(line.strip() for line in body.splitlines() if not line.lstrip().startswith("#"))
                self.assertGreaterEqual(len(content), 200)
                self.assertLessEqual(len(body.encode("utf-8")), 8192)
                self.assertNotRegex(body, r"/Users/|F:\\qgis_project|\b(?:orchestrator|data_engineer|cartographer|vector_spatial|data_processing|raster_terrain|cartography_layout)\b")

    def test_reference_links_exist_and_contain_usable_methods(self):
        for source, (target, title) in REFERENCE_METHODS.items():
            with self.subTest(source=source):
                path = "references/" + title + ".md"
                self.assertIn(path, package(target)[1])
                text = (ROOT / target / path).read_text(encoding="utf-8")
                self.assertGreater(len(text), 200)
                self.assertNotRegex(text, r"\[(?:orchestrator|data_processing|vector_spatial|raster_terrain|cartography_layout)\]")

    def test_gee_licensed_assets_preserve_frozen_original_bytes(self):
        manifest = json.loads((ROOT.parents[1]/"tests/fixtures/gee_licensed_assets.json").read_text())
        self.assertFalse((ROOT/"gee-scripting").exists())
        self.assertEqual(set(manifest), {"LICENSE", "references/change-detection.md", "references/classification.md",
                                         "references/image-composite.md", "templates/single_date_image.py", "templates/single-date-image.md"})
        for relative, expected in manifest.items():
            with self.subTest(path=relative):
                self.assertEqual(hashlib.sha256((ROOT/"gee-remote-sensing"/relative).read_bytes()).hexdigest(), expected)
        self.assertTrue((ROOT/"gee-remote-sensing/UPSTREAM.md").is_file())

    def test_only_formal_categories_remain_without_canvas_override(self):
        retired = json.loads((ROOT.parents[1]/"tests/fixtures/retired_skill_packages.json").read_text())
        self.assertEqual(len(retired), 57)
        for name in retired:
            self.assertFalse((ROOT/name).exists(), name)
        for path in ROOT.glob("*/SKILL.md"):
            front, _ = package(path.parent.name)
            self.assertEqual(front["kind"], "category", path)
            self.assertIsInstance(front.get("enabled", True), bool)
        self.assertTrue(package("canvas-navigation")[0]["enabled"])
        self.assertFalse((ROOT/"canvas-navigation/config.yaml").exists())

    def test_all_category_bodies_fit_budget(self):
        for source in sorted(ROOT.glob("*/SKILL.md")):
            front, body = package(source.parent.name)
            if front.get("kind") == "category":
                with self.subTest(package=source.parent.name):
                    self.assertLessEqual(len(body.encode("utf-8")), 8192)


if __name__ == "__main__":
    unittest.main()
