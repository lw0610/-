import unittest
from pathlib import Path


class ScaleBarLayoutContractTestCase(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.workspace = Path(__file__).resolve().parents[2]

    def test_reference_alignment_is_exposed_to_layout_agent(self):
        tool_path = (
            self.workspace
            / "autocarto/tools/system/print-layout/layout_align_items.yaml"
        )
        tool_text = tool_path.read_text(encoding="utf-8")

        self.assertIn("- name: reference_item_id", tool_text)
        self.assertIn("reference_item_id: '{{ reference_item_id }}'", tool_text)

        category_text = (
            self.workspace / "autocarto/skills/system/print-layout/SKILL.md"
        ).read_text(encoding="utf-8")
        self.assertIn("- layout_align_items", category_text)

    def test_marching_route_scalebar_uses_actual_size_and_reference_alignment(self):
        source_path = (
            self.workspace
            / "autocarto/skills/system/print-layout-marching-route/SKILL.md"
        )
        embedded_path = (
            self.workspace
            / "carto_agent/defaults/skills/print-layout-marching-route/SKILL.md"
        )
        source = source_path.read_text(encoding="utf-8").replace("\r\n", "\n")
        embedded = embedded_path.read_text(encoding="utf-8").replace("\r\n", "\n")

        self.assertEqual(source, embedded)
        self.assertNotIn("比例尺宽度固定为 80mm", source)
        self.assertIn("最终元素外框宽度以 QGIS 返回的实际", source)
        self.assertIn('reference_item_id="main_map"', source)
        self.assertIn("比例尺是例外：不得使用 `layout_resize_item`", source)

    def test_scalebar_handler_uses_qgis_fit_width_and_reports_actual_resize(self):
        implementation = (
            self.workspace
            / "autocarto/tools/system/_shared/print_layout_items_impl.py"
        ).read_text(encoding="utf-8")

        self.assertIn('"ScaleBarSegmentSizeMode"', implementation)
        self.assertIn('"FitWidth"', implementation)
        self.assertIn("setMinimumBarWidth", implementation)
        self.assertIn("setMaximumBarWidth", implementation)
        self.assertIn('"requested_size_mm"', implementation)
        self.assertIn('"auto_adjusted": not size_matched', implementation)

    def test_scalebar_uses_the_single_visible_main_map(self):
        implementation = (
            self.workspace
            / "autocarto/tools/system/_shared/print_layout_items_impl.py"
        ).read_text(encoding="utf-8")

        self.assertNotIn("__scalebar_ref_", implementation)
        self.assertNotIn("setExcludeFromExports(True)", implementation)
        self.assertNotIn("_auto_projected_crs_for_map", implementation)
        self.assertNotIn("map_item.setCrs(target_crs)", implementation)
        self.assertIn("map_item.setCrs(project.crs())", implementation)
        self.assertIn("map_item.crs().isGeographic()", implementation)
        self.assertIn("Qgis.DistanceUnit.Kilometers", implementation)
        self.assertIn("scalebar.setLinkedMap(map_item)", implementation)
        self.assertIn("map_item=map_item", implementation)

    def test_map_frame_matches_the_source_extent_aspect_ratio(self):
        implementation = (
            self.workspace
            / "autocarto/tools/system/_shared/print_layout_items_impl.py"
        ).read_text(encoding="utf-8")

        self.assertIn("def _map_frame_size_for_extent", implementation)
        self.assertIn("extent_ratio = extent_width / extent_height", implementation)
        self.assertIn("width, height = _map_frame_size_for_extent", implementation)

    def test_cross_crs_vector_extent_uses_actual_geometries(self):
        implementation = (
            self.workspace
            / "autocarto/tools/system/_shared/print_layout_items_impl.py"
        ).read_text(encoding="utf-8")

        self.assertIn("def _get_transformed_vector_extent", implementation)
        self.assertIn("source_geometry = feature.geometry()", implementation)
        self.assertIn("geometry.transform(transformer)", implementation)
        self.assertIn(
            "exact_extent = _get_transformed_vector_extent(layer, transformer)",
            implementation,
        )

    def test_grid_defaults_to_project_crs_when_layout_map_is_projected(self):
        implementation = (
            self.workspace
            / "autocarto/tools/system/_shared/print_layout_items_impl.py"
        ).read_text(encoding="utf-8")

        self.assertIn("default_grid_crs = project.crs()", implementation)
        self.assertIn("grid.setCrs(default_grid_crs)", implementation)


if __name__ == "__main__":
    unittest.main()
