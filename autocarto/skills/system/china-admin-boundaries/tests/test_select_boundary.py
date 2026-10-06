"""Offline source-integrity and actual polygon-selection regressions."""

import copy
import hashlib
import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest import mock


PACKAGE = Path(__file__).resolve().parents[1]
TEMPLATE = PACKAGE / "templates/select_boundary.py"
spec = importlib.util.spec_from_file_location("select_boundary", TEMPLATE)
selector = importlib.util.module_from_spec(spec)
spec.loader.exec_module(selector)


class BoundaryTemplateTests(unittest.TestCase):
    def test_full_upstream_assets_and_all_geometries_are_intact(self):
        from shapely.geometry import shape
        manifest = selector._manifest()
        for asset in manifest["assets"]:
            data = (PACKAGE / asset["local_path"]).read_bytes()
            self.assertEqual(len(data), asset["bytes"])
            self.assertEqual(hashlib.sha256(data).hexdigest(), asset["sha256"])
            self.assertEqual(hashlib.sha1(("blob " + str(len(data)) + "\0").encode() + data).hexdigest(), asset["git_blob_sha1"])
        for level, expected in (("province", 34), ("city", 375), ("county", 2891)):
            features, _ = selector._load_layer(level)
            self.assertEqual(len(features), expected)
            for feature in features:
                with self.subTest(level=level, adcode=feature["properties"]["adcode"]):
                    geometry = shape(feature["geometry"])
                    self.assertFalse(geometry.is_empty)
                    self.assertTrue(geometry.is_valid)
                    selector._geometry_report(feature)

    def test_actual_wuhan_export_preserves_geometry_and_provenance(self):
        with tempfile.TemporaryDirectory() as directory:
            result = selector.export_boundary(directory, name="武汉市", level="city")
            document = json.loads(Path(result["boundary_path"]).read_text())
            original = next(f for f in selector._load_layer("city")[0] if f["properties"]["adcode"] == "420100")
            self.assertEqual(document["features"], [original])
            self.assertEqual(result["selected"]["name"], "武汉市")
            self.assertEqual(result["geometry"]["type"], "MultiPolygon")
            self.assertEqual(result["geometry"]["vertices"], 369)
            provenance = json.loads(Path(result["provenance_path"]).read_text())
            self.assertEqual(provenance["commit"], selector.SOURCE_COMMIT)
            self.assertEqual(provenance["source"]["sha256"], "a3038b09823597c5b03ea2563455cebcc90559e83764f5d49d38a1403ad8683a")
            self.assertEqual(provenance["selection"], result["selected"])
            self.assertEqual(len(list(Path(directory).iterdir())), 2)
            # Exercise the real GEE boundary reader offline, without EE calls.
            worker_path = PACKAGE.parents[3] / "carto_agent/internal/geeruntime/worker.py"
            worker_spec = importlib.util.spec_from_file_location("gee_boundary_reader", worker_path)
            worker = importlib.util.module_from_spec(worker_spec)
            worker_spec.loader.exec_module(worker)
            geometry = worker.read_region_geometry(result["boundary_path"])
            self.assertEqual(geometry, original["geometry"])

    def test_same_names_return_candidates_and_do_not_create_output(self):
        candidates = selector.find_candidates(name="青山区", level="county")
        self.assertEqual({item["adcode"] for item in candidates}, {"150204", "420107"})
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaises(selector.BoundarySelectionError) as caught:
                selector.export_boundary(directory, name="青山区", level="county")
            self.assertEqual(caught.exception.code, "boundary_ambiguous")
            self.assertEqual(caught.exception.candidates, candidates)
            self.assertEqual(list(Path(directory).iterdir()), [])
            result = selector.export_boundary(directory, adcode="420107", expected_name="青山区", level="county")
            self.assertEqual(result["selected"]["adcode"], "420107")

    def test_query_does_not_export_or_guess_abbreviation(self):
        self.assertEqual(selector.find_candidates(name="武汉", level="city"), [])
        self.assertEqual(selector.find_candidates(name="武汉", level="city", contains=True)[0]["name"], "武汉市")
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaises(selector.BoundarySelectionError) as caught:
                selector.export_boundary(directory, name="不存在的行政区", level="city")
            self.assertEqual(caught.exception.code, "boundary_not_found")
            self.assertEqual(list(Path(directory).iterdir()), [])

    def test_wrong_name_code_level_and_output_path_are_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            for kwargs, code in (({"adcode": "420100", "level": "city", "expected_name": "北京市"}, "boundary_name_mismatch"),
                                 ({"adcode": "4201"}, "invalid_arguments"),
                                 ({"adcode": 420100}, "invalid_arguments"),
                                 ({"name": "武汉市", "level": "unknown"}, "invalid_arguments"),
                                 ({"name": "武汉市", "filename": "../escape.geojson"}, "invalid_arguments"),
                                 ({"name": "武汉市", "filename": 0}, "invalid_arguments")):
                with self.subTest(kwargs=kwargs), self.assertRaises(selector.BoundarySelectionError) as caught:
                    selector.export_boundary(directory, **kwargs)
                self.assertEqual(caught.exception.code, code)
            self.assertEqual(list(Path(directory).iterdir()), [])

    def test_source_schema_crs_and_duplicate_identity_are_rejected(self):
        feature = selector._load_layer("city")[0][0]
        original = {"type": "FeatureCollection", "crs": {"type": "name", "properties": {"name": selector.CRS84}}, "features": [feature]}
        variants = []
        bad = copy.deepcopy(original); bad["crs"] = None; variants.append(bad)
        bad = copy.deepcopy(original); bad["crs"]["properties"]["name"] = "GCJ-02"; variants.append(bad)
        bad = copy.deepcopy(original); bad["features"] *= 2; variants.append(bad)
        bad = copy.deepcopy(original); bad["features"][0]["properties"]["gb"] = "wrong"; variants.append(bad)
        bad = copy.deepcopy(original); bad["features"][0]["geometry"] = None; variants.append(bad)
        for value in variants:
            with self.subTest(value=value), self.assertRaises(selector.BoundarySelectionError) as caught:
                selector._validate_layer(value, "city")
            self.assertEqual(caught.exception.code, "boundary_source_invalid")

    def test_changed_source_is_not_silently_used(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory); (root / "data").mkdir()
            (root / "upstream-manifest.json").write_bytes((PACKAGE / "upstream-manifest.json").read_bytes())
            (root / "data/china_city.geojson").write_bytes((PACKAGE / "data/china_city.geojson").read_bytes() + b" ")
            with mock.patch.object(selector, "PACKAGE_DIR", root):
                with self.assertRaises(selector.BoundarySelectionError) as caught:
                    selector.find_candidates(name="武汉市", level="city")
                self.assertEqual(caught.exception.code, "boundary_source_invalid")

    def test_invalid_polygon_is_not_repaired(self):
        feature = {"geometry": {"type": "Polygon", "coordinates": [[[114, 30], [115, 31], [114, 31], [115, 30], [114, 30]]]}}
        with self.assertRaises(selector.BoundarySelectionError) as caught:
            selector._geometry_report(feature)
        self.assertEqual(caught.exception.code, "boundary_geometry_invalid")

    def test_existing_output_is_preserved(self):
        with tempfile.TemporaryDirectory() as directory:
            result = selector.export_boundary(directory, name="武汉市", level="city")
            content = Path(result["boundary_path"]).read_bytes()
            with self.assertRaises(selector.BoundarySelectionError) as caught:
                selector.export_boundary(directory, name="武汉市", level="city")
            self.assertEqual(caught.exception.code, "output_exists")
            self.assertEqual(Path(result["boundary_path"]).read_bytes(), content)

    def test_cli_reports_real_output_or_ambiguity_without_network(self):
        with tempfile.TemporaryDirectory() as directory:
            result = subprocess.run([sys.executable, str(TEMPLATE), "--name", "武汉市", "--level", "city", "--output-dir", directory], capture_output=True, text=True, check=True)
            self.assertTrue(Path(json.loads(result.stdout)["boundary_path"]).is_file())
            ambiguity = subprocess.run([sys.executable, str(TEMPLATE), "--name", "青山区", "--level", "county", "--output-dir", directory], capture_output=True, text=True)
            self.assertEqual(ambiguity.returncode, 2)
            self.assertEqual(len(json.loads(ambiguity.stdout)["candidates"]), 2)


if __name__ == "__main__":
    unittest.main()
