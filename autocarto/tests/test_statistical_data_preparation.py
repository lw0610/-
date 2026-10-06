"""Statistical joins are tested without QGIS or a user's live project."""
import csv
import json
from pathlib import Path
import tempfile
import unittest
from unittest import mock

from autocarto.tools.system._shared.statistical_data_impl import prepare_statistical_handler


def polygon(name, x=0):
    return {"type": "Feature", "properties": {"name": name, "original": "preserved"},
            "geometry": {"type": "Polygon", "coordinates": [[[x, 0], [x + 1, 0], [x + 1, 1], [x, 1], [x, 0]]]}}


class StatisticalDataPreparationTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="autocarto-statistical-test-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.geo = self.root / "source.geojson"
        self.csv = self.root / "source.csv"
        self.output = self.root / "outputs"
        self.write_geo([polygon("甲"), polygon("乙", 1), polygon("丙", 2)])
        self.write_csv([["年份", "指标", "单位", "数据来源", "数据库", "甲", "乙", "丙"],
                        [2023, "居民数", "人", "公开统计表", "公开库", 10, 20, 30],
                        [2024, "居民数", "人", "公开统计表", "公开库", 0, "", 30]])

    def write_geo(self, features, **extra):
        self.geo.write_text(json.dumps({"type": "FeatureCollection", "features": features, **extra}, ensure_ascii=False), encoding="utf-8")

    def write_csv(self, rows):
        with self.csv.open("w", encoding="utf-8-sig", newline="") as handle:
            csv.writer(handle).writerows(rows)

    def run_tool(self, **kwargs):
        return prepare_statistical_handler({"geojson_path": str(self.geo), "csv_path": str(self.csv), "year": 2024, "output_dir": str(self.output), **kwargs})

    def test_outputs_preserve_zero_null_metadata_geometry_and_input_bytes(self):
        boundary = {"type": "Feature", "properties": {"boundary": "border"}, "geometry": {"type": "LineString", "coordinates": [[0, 0], [3, 0]]}}
        crs = {"type": "name", "properties": {"name": "EPSG:4326"}}
        self.write_geo([polygon("甲"), polygon("乙", 1), polygon("丙", 2), boundary], crs=crs)
        source_bytes = self.geo.read_bytes(), self.csv.read_bytes()
        result = self.run_tool(exclude_names=["丙"])
        self.assertTrue(result["success"], result)
        report = result["data"]
        self.assertEqual(report["metadata"]["indicator"], "居民数")
        self.assertEqual(report["counts"]["boundary_count"], 1)
        paths = report["paths"]
        derived = json.loads(Path(paths["polygons_geojson"]).read_text())
        self.assertEqual(derived["crs"], crs)
        attrs = [f["properties"] for f in derived["features"]]
        self.assertEqual([a["stat_value"] for a in attrs], [0.0, None, None])
        self.assertEqual([a["data_status"] for a in attrs], ["valid", "missing", "excluded"])
        self.assertEqual(attrs[0]["stat_source"], "公开统计表")
        self.assertEqual(attrs[0]["original"], "preserved")
        self.assertEqual(derived["features"][0]["geometry"], polygon("甲")["geometry"])
        self.assertEqual(json.loads(Path(paths["boundaries_geojson"]).read_text())["features"], [boundary])
        with Path(paths["long_csv"]).open(encoding="utf-8-sig", newline="") as handle:
            rows = list(csv.DictReader(handle))
        self.assertEqual([r["stat_value"] for r in rows], ["0.0", "", ""])
        self.assertEqual(rows[2]["raw_value"], "30")
        manifest = json.loads(Path(paths["manifest"]).read_text())
        self.assertEqual(manifest["paths"], paths)
        self.assertEqual(len(manifest["inputs"]["csv"]["sha256"]), 64)
        self.assertEqual((self.geo.read_bytes(), self.csv.read_bytes()), source_bytes)

    def test_validate_only_never_creates_output_or_default_home(self):
        with mock.patch.dict("os.environ", {"AUTOCARTO_HOME": str(self.root / "unused_home")}):
            result = self.run_tool(validate_only=True, output_dir=None)
        self.assertTrue(result["success"], result)
        self.assertEqual(result["data"]["paths"], {})
        self.assertEqual(set(self.root.iterdir()), {self.geo, self.csv})
        self.assertEqual(result["data"]["missing_regions"], ["乙"])

    def test_unique_run_directories_and_default_home(self):
        first = self.run_tool()
        second = self.run_tool()
        self.assertTrue(first["success"], first)
        self.assertTrue(second["success"], second)
        self.assertNotEqual(first["output"], second["output"])
        original = Path(first["output"]).read_bytes()
        with mock.patch.dict("os.environ", {"AUTOCARTO_HOME": str(self.root / "home")}):
            default = self.run_tool(output_dir=None)
        self.assertTrue(default["success"], default)
        self.assertTrue(Path(default["output"]).is_relative_to((self.root / "home" / "data" / "statistical").resolve()))
        self.assertEqual(Path(first["output"]).read_bytes(), original)

    def test_injected_current_session_controls_default_output(self):
        session = self.root / "sessions" / "current"
        subdir = session / "intermediate"
        home = self.root / "home"
        with mock.patch.dict("os.environ", {"AUTOCARTO_HOME": str(home)}):
            validated = self.run_tool(output_dir=None, validate_only=True, session_dir=str(session))
            self.assertTrue(validated["success"], validated)
            self.assertFalse(session.exists())
            result = self.run_tool(output_dir=None, session_dir=str(session), session_subdirs={"intermediate": str(subdir)}, session_id="current", project_path="/synthetic/project.qgz")
            self.assertTrue(result["success"], result)
            self.assertTrue(Path(result["output"]).is_relative_to((subdir / "statistical").resolve()))
            self.assertFalse(home.exists())
            manifest = json.loads(Path(result["output"]).read_text())
            self.assertEqual(manifest["session_id"], "current")
            self.assertEqual(manifest["project_path"], "/synthetic/project.qgz")
            fallback = self.run_tool(output_dir=None, session_dir=str(session))
            self.assertTrue(fallback["success"], fallback)
            self.assertTrue(Path(fallback["output"]).is_relative_to((subdir / "statistical").resolve()))
            explicit = self.run_tool(session_dir=str(session))
            self.assertTrue(explicit["success"], explicit)
            self.assertTrue(Path(explicit["output"]).is_relative_to(self.output.resolve()))
            stale = self.root / "stale-session"
            mismatch = self.run_tool(output_dir=None, session_dir=str(session), session_subdirs={"intermediate": str(stale)})
            self.assertFalse(mismatch["success"])
            self.assertFalse(stale.exists())

    def test_mismatched_names_return_full_report_and_require_explicit_opt_in(self):
        self.write_csv([["年份", "甲", "错误简称"], [2024, 1, 2]])
        result = self.run_tool()
        self.assertFalse(result["success"])
        self.assertEqual(result["data"]["unmatched_csv_regions"], ["错误简称"])
        self.assertEqual(result["data"]["unmatched_geojson_regions"], ["丙", "乙"])
        self.assertFalse(self.output.exists())
        permitted = self.run_tool(allow_unmatched=True)
        self.assertTrue(permitted["success"], permitted)
        attrs = [f["properties"] for f in json.loads(Path(permitted["data"]["paths"]["polygons_geojson"]).read_text())["features"]]
        self.assertEqual(attrs[1]["stat_value"], None)
        self.assertEqual(attrs[1]["data_status"], "unmatched")

    def test_duplicate_years_headers_and_geometry_keys_fail_before_output(self):
        cases = [
            [["年份", "甲", "乙", "丙"], [2024, 1, 2, 3], [2024, 4, 5, 6]],
            [["年份", "甲", " 甲", "丙"], [2024, 1, 2, 3]],
            [["年份", "甲", "乙", "丙"], [2024, 1, 2]],
        ]
        for rows in cases:
            with self.subTest(rows=rows):
                self.write_csv(rows)
                self.assertFalse(self.run_tool()["success"])
                self.assertFalse(self.output.exists())
        self.write_csv([["年份", "甲"], [2024, 1]])
        self.write_geo([polygon("甲"), polygon(" 甲 ", 1)])
        self.assertIn("重复", self.run_tool()["info"])
        self.assertFalse(self.output.exists())

    def test_invalid_numeric_values_are_not_coerced_to_zero(self):
        for raw in ["NaN", "inf", "1e999", "10%", "12万元", "1,2", "true", "=1+2"]:
            with self.subTest(raw=raw):
                self.write_csv([["年份", "甲", "乙", "丙"], [2024, raw, 2, 3]])
                self.assertFalse(self.run_tool()["success"])
                self.assertFalse(self.output.exists())
        self.write_csv([["年份", "甲", "乙", "丙"], [2024, "1,234.5", "—", "-2.5"]])
        valid = self.run_tool(validate_only=True)
        self.assertTrue(valid["success"], valid)
        self.assertEqual(valid["data"]["value_range"], {"min": -2.5, "max": 1234.5})
        self.assertEqual(valid["data"]["missing_regions"], ["乙"])

    def test_custom_metadata_mapping_and_explicit_values(self):
        self.write_csv([["年份", "measure", "甲", "乙", "丙"], [2024, "失业率", 1, 2, 3]])
        result = self.run_tool(validate_only=True, metadata_fields={"indicator": "measure"}, unit="%", source="统计公告")
        self.assertTrue(result["success"], result)
        self.assertEqual(result["data"]["metadata"], {"indicator": "失业率", "unit": "%", "source": "统计公告", "database": None})
        self.assertEqual(result["data"]["counts"]["csv_region_count"], 3)
        conflict = self.run_tool(metadata_fields={"indicator": "measure"}, indicator="人口")
        self.assertFalse(conflict["success"])
        self.assertIn("冲突", conflict["info"])
        self.assertFalse(self.output.exists())

    def test_long_format_keys_and_consistent_metadata(self):
        rows = [["年份", "地区", "数值", "单位"], [2024, " 甲 ", 0, "人"], [2024, "乙", "N/A", "人"], [2024, "丙", 3, "人"], [2023, "甲", 4, "人"]]
        self.write_csv(rows)
        result = self.run_tool(table_format="long", validate_only=True)
        self.assertTrue(result["success"], result)
        self.assertEqual(result["data"]["metadata"]["unit"], "人")
        self.assertEqual(result["data"]["counts"]["valid_count"], 2)
        self.write_csv(rows + [[2024, "甲", 8, "人"]])
        duplicate = self.run_tool(table_format="long")
        self.assertIn("重复年份/地区键", duplicate["info"])
        rows[2][-1] = "万人"
        self.write_csv(rows)
        inconsistent = self.run_tool(table_format="long")
        self.assertFalse(inconsistent["success"])
        self.assertIn("不一致", inconsistent["info"])
        self.assertFalse(self.output.exists())

    def test_field_collision_exclusion_typos_and_invalid_options_fail(self):
        for kwargs in [{"value_field": "name"}, {"value_field": "data_status"}, {"exclude_names": ["未知地区"]}, {"allow_unmatched": "true"}, {"table_format": "guess"}, {"year": True}, {"year": 2025}, {"output_dir": "relative"}]:
            with self.subTest(kwargs=kwargs):
                self.assertFalse(self.run_tool(**kwargs)["success"])
                self.assertFalse(self.output.exists())
        feature = polygon("甲")
        feature["properties"]["stat_year"] = 2001
        self.write_geo([feature])
        self.assertIn("冲突", self.run_tool()["info"])

    def test_non_spatial_and_unclosed_geojson_rejected_without_output(self):
        for geometry in [{"type": "Point", "coordinates": [0, 0]}, {"type": "Polygon", "coordinates": [[[0, 0], [1, 0], [1, 1], [0, 1]]]}, None]:
            with self.subTest(geometry=geometry):
                feature = polygon("甲")
                feature["geometry"] = geometry
                self.write_geo([feature])
                self.assertFalse(self.run_tool()["success"])
                self.assertFalse(self.output.exists())


if __name__ == "__main__":
    unittest.main()
