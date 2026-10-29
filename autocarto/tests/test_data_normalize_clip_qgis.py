"""Offline clipping contracts, runnable with QGIS Python and stdlib unittest.

Uses an isolated QGIS application and synthetic local data only. No application
session, user's project, network request, or global Python installation is used.
"""
import importlib
import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from autocarto.tests.golden.qgis_bootstrap import isolated_qgis, qgis_available


def feature(geometry, properties=None):
    return {"type": "Feature", "properties": properties or {}, "geometry": geometry}


def polygon(rings):
    return {"type": "Polygon", "coordinates": rings}


SHELL = [[0, 0], [4, 0], [4, 4], [0, 4], [0, 0]]
HOLE = [[1, 1], [1, 3], [3, 3], [3, 1], [1, 1]]
ISLAND = [[6, 0], [8, 0], [8, 4], [6, 4], [6, 0]]


@unittest.skipUnless(qgis_available(), "requires QGIS Python")
class NormalizeClipQGISTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.runtime_dir = tempfile.TemporaryDirectory(prefix="normalize-clip-runtime-")
        cls.runtime = isolated_qgis(Path(cls.runtime_dir.name))
        cls.runtime.__enter__()
        cls.module = importlib.import_module("autocarto.tools.system.data-acquisition.handlers.normalize")

    @classmethod
    def tearDownClass(cls):
        cls.runtime.__exit__(None, None, None)
        cls.runtime_dir.cleanup()

    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="normalize-clip-case-")
        self.root = Path(self.temporary.name)
        self.addCleanup(self.temporary.cleanup)

    def write(self, name, features, crs=None):
        path = self.root / name
        value = {"type": "FeatureCollection", "features": features}
        if crs:
            value["crs"] = {"type": "name", "properties": {"name": crs}}
        path.write_text(json.dumps(value, ensure_ascii=False), encoding="utf-8")
        return path

    def boundary(self):
        return self.write("boundary.geojson", [feature({
            "type": "MultiPolygon", "coordinates": [[SHELL, HOLE], [ISLAND]],
        })])

    def source_line(self):
        return self.write("source.geojson", [feature({
            "type": "LineString", "coordinates": [[-1, 2], [9, 2]],
        }, {"name:zh": "跨界线", "osm_id": 123456789012345, "ref": "G123"})])

    def test_invalid_vector_reports_vector_open_error(self):
        source = self.root / "broken.osm.pbf"
        source.write_bytes(b"invalid pbf")
        result = self.run_normalize(source, expected_geometry="line")
        self.assertFalse(result["success"], result)
        self.assertIn("无法打开原始矢量数据", result["message"])
        self.assertNotIn("栅格", result["message"])

    def test_osm_hstore_preserves_newlines_and_escaped_characters(self):
        from qgis.core import QgsVectorLayer
        source = self.write("hstore.geojson", [feature({"type": "LineString", "coordinates": [[0, 0], [1, 1]]}, {
            "other_tags": '"name:zh"=>"沅江", "note"=>"第一行\n第二行", "ref"=>"A\\\\B\\\"C"',
        })])
        result = self.run_normalize(source, expected_geometry="line", fields={"name": ["name:zh"], "note": ["note"], "ref": ["ref"]})
        self.assertTrue(result["success"], result)
        row = next(QgsVectorLayer(result["output"], "result", "ogr").getFeatures())
        self.assertEqual(row["name"], "沅江")
        self.assertEqual(row["note"], "第一行\n第二行")
        self.assertEqual(row["ref"], 'A\\B"C')

    def test_geopackage_split_features_get_new_primary_keys(self):
        from qgis.core import QgsVectorLayer
        source = self.write("fid.geojson", [
            feature({"type": "LineString", "coordinates": [[0, 0], [1, 1]]}, {"fid": 11, "name": "同名线路"}),
            feature({"type": "LineString", "coordinates": [[3, 3], [4, 4]]}, {"fid": 12, "name": "同名线路"}),
        ])
        base = self.root / "base.gpkg"
        result = self.run_normalize(source, expected_geometry="line", output=str(base))
        self.assertTrue(result["success"], result)
        result = self.run_normalize(base, expected_geometry="line", merge_by="name", output=str(self.root / "derived.gpkg"))
        self.assertTrue(result["success"], result)
        layer = QgsVectorLayer(result["output"], "derived", "ogr")
        rows = list(layer.getFeatures())
        self.assertEqual(len(rows), 2)
        self.assertEqual(len({row.id() for row in rows}), 2)
        self.assertEqual([row["source_fid"] for row in rows], [11, 11])
        # A result must remain usable by subsequent real QGIS algorithms, which
        # conventionally use the fid column as their GeoPackage primary key.
        import processing
        saved = processing.run("native:savefeatures", {"INPUT": layer, "OUTPUT": str(self.root / "saved.gpkg")})
        self.assertEqual(QgsVectorLayer(saved["OUTPUT"], "saved", "ogr").featureCount(), 2)

    def run_normalize(self, source, boundary=None, **kwargs):
        params = {
            "input": {"path": str(source)}, "output": str(self.root / "result.geojson"),
            "fields": {}, "standard_fields": [], "simplify_tolerance_m": 0,
        }
        if boundary is not None:
            params["clip"] = {"path": str(boundary)}
        params.update(kwargs)
        return self.module.normalize_handler(params)

    def assert_failed_cleanly(self, result, originals):
        self.assertFalse(result["success"], result)
        self.assertFalse(result.get("data", {}).get("clip_applied", False), result)
        self.assertFalse((self.root / "result.geojson").exists())
        self.assertEqual(list(self.root.glob(".normalize-*")), [])
        for path, content in originals.items():
            self.assertEqual(path.read_bytes(), content)

    def test_multipart_hole_crossing_line_keeps_all_properties(self):
        from qgis.core import QgsGeometry, QgsProject, QgsVectorLayer
        properties = {"name:zh": "跨界铁路", "osm_id": 123456789012345,
                      "active": True, "width": 3.5, "missing": None,
                      "other_tags": '"railway:class"=>"main"'}
        source = self.write("source.geojson", [
            feature({"type": "LineString", "coordinates": [[-1, 2], [9, 2]]}, properties),
            feature({"type": "LineString", "coordinates": [[-1, 6], [9, 6]]}, properties),
        ])
        boundary = self.boundary()
        originals = {path: path.read_bytes() for path in (source, boundary)}
        result = self.run_normalize(source, boundary)
        self.assertTrue(result["success"], result)
        data = result["data"]
        self.assertTrue(data["clip_applied"])
        self.assertEqual(data["clip_kind"], "polygon")
        self.assertEqual(data["clip_path"], str(boundary.resolve()))
        self.assertEqual((data["clip_input_features"], data["clip_empty_features"], data["clip_output_features"]), (2, 1, 1))
        self.assertEqual(set(data["fields"]), set(properties))
        output = Path(data["path"])
        rows = json.loads(output.read_text())["features"]
        self.assertEqual(len(rows), 1)
        self.assertEqual(rows[0]["properties"], properties)
        layer = QgsVectorLayer(str(output), "result", "ogr")
        actual = next(layer.getFeatures()).geometry()
        expected = QgsGeometry.fromWkt("MULTILINESTRING((0 2,1 2),(3 2,4 2),(6 2,8 2))")
        self.assertTrue(actual.isGeosEqual(expected), actual.asWkt())
        self.assertFalse(QgsProject.instance().mapLayers())
        for path, content in originals.items():
            self.assertEqual(path.read_bytes(), content)

    def test_polygon_intersection_preserves_hole_and_island(self):
        from qgis.core import QgsGeometry, QgsVectorLayer
        source = self.write("cover.geojson", [feature(polygon([
            [[-1, -1], [9, -1], [9, 5], [-1, 5], [-1, -1]],
        ]), {"name": "cover"})])
        result = self.run_normalize(source, self.boundary())
        self.assertTrue(result["success"], result)
        layer = QgsVectorLayer(result["data"]["path"], "result", "ogr")
        geometry = next(layer.getFeatures()).geometry()
        expected = QgsGeometry.fromWkt("MULTIPOLYGON(((0 0,4 0,4 4,0 4,0 0),(1 1,1 3,3 3,3 1,1 1)),((6 0,8 0,8 4,6 4,6 0)))")
        self.assertTrue(geometry.isGeosEqual(expected), geometry.asWkt())
        self.assertAlmostEqual(geometry.area(), 20)

    def test_disjoint_is_successful_empty_result(self):
        source = self.write("outside.geojson", [feature({
            "type": "LineString", "coordinates": [[20, 20], [21, 21]],
        }, {"name": "outside"})])
        result = self.run_normalize(source, self.boundary())
        self.assertTrue(result["success"], result)
        self.assertTrue(result["data"]["clip_applied"])
        self.assertEqual(result["data"]["count"], 0)
        self.assertEqual(result["data"]["clip_empty_features"], 1)
        self.assertEqual(json.loads(Path(result["data"]["path"]).read_text())["features"], [])

    def test_touching_only_does_not_change_output_geometry_family(self):
        cases = {
            "line_corner": {"type": "LineString", "coordinates": [[-1, -1], [0, 0]]},
            "polygon_edge": polygon([[[-1, 0], [0, 0], [0, 1], [-1, 1], [-1, 0]]]),
        }
        boundary = self.write("square.geojson", [feature(polygon([SHELL]))])
        for label, geometry in cases.items():
            with self.subTest(label=label):
                source = self.write(label + ".geojson", [feature(geometry, {"name": label})])
                result = self.run_normalize(source, boundary, output=str(self.root / (label + "-clipped.geojson")))
                self.assertTrue(result["success"], result)
                self.assertEqual(result["data"]["count"], 0)
                self.assertEqual(result["data"]["clip_nonmatching_geometry_features"], 1)
                self.assertEqual(json.loads(Path(result["data"]["path"]).read_text())["features"], [])

    def test_mixed_intersection_uses_qgis_to_keep_line_members(self):
        from qgis.core import QgsGeometry, QgsVectorLayer
        boundary = self.write("square.geojson", [feature(polygon([SHELL]))])
        source = self.write("mixed.geojson", [feature({
            "type": "MultiLineString", "coordinates": [[[-1, 2], [2, 2]], [[-1, -1], [0, 0]]],
        }, {"id": "mixed"})])
        result = self.run_normalize(source, boundary)
        self.assertTrue(result["success"], result)
        layer = QgsVectorLayer(result["data"]["path"], "result", "ogr")
        actual = next(layer.getFeatures()).geometry()
        self.assertTrue(actual.isGeosEqual(QgsGeometry.fromWkt("MULTILINESTRING((0 2,2 2))")), actual.asWkt())
        self.assertEqual(result["data"]["count"], 1)

    def test_invalid_or_empty_boundary_fails_without_publishing(self):
        cases = {
            "self_intersection": [feature(polygon([[[0, 0], [4, 4], [4, 0], [0, 4], [0, 0]]]))],
            "null": [feature(None)],
            "empty": [],
            "not_polygon": [feature({"type": "LineString", "coordinates": [[0, 0], [4, 4]]})],
        }
        source = self.source_line()
        for label, features in cases.items():
            with self.subTest(label=label):
                boundary = self.write(label + ".geojson", features)
                originals = {path: path.read_bytes() for path in (source, boundary)}
                self.assert_failed_cleanly(self.run_normalize(source, boundary), originals)

    def test_invalid_source_geometry_cannot_be_silently_dropped(self):
        source = self.write("invalid.geojson", [feature(polygon([
            [[0, 0], [4, 4], [4, 0], [0, 4], [0, 0]],
        ]))])
        before = source.read_bytes()
        self.assert_failed_cleanly(self.run_normalize(source, self.boundary()), {source: before})

    def test_geos_failure_is_not_empty_intersection(self):
        from qgis.core import QgsGeometry

        class FailedGeometry:
            def lastError(self):
                return "injected GEOS topology failure"

        source = self.write("source.geojson", [
            feature({"type": "LineString", "coordinates": [[-1, 2], [9, 2]]}, {"id": "first"}),
            feature({"type": "LineString", "coordinates": [[-1, .5], [9, .5]]}, {"id": "second"}),
        ])
        boundary = self.boundary()
        originals = {path: path.read_bytes() for path in (source, boundary)}
        original_intersection = QgsGeometry.intersection
        calls = []

        def interrupted(geometry, other):
            calls.append(1)
            return original_intersection(geometry, other) if len(calls) == 1 else FailedGeometry()

        with patch.object(QgsGeometry, "intersection", interrupted):
            result = self.run_normalize(source, boundary)
        self.assertEqual(len(calls), 2)
        self.assertIn("injected GEOS", result["message"])
        self.assert_failed_cleanly(result, originals)

    def test_null_intersection_without_error_is_not_success(self):
        from qgis.core import QgsGeometry
        source, boundary = self.source_line(), self.boundary()
        with patch.object(QgsGeometry, "intersection", return_value=QgsGeometry()):
            result = self.run_normalize(source, boundary)
        self.assert_failed_cleanly(result, {source: source.read_bytes()})

    def test_union_failure_cannot_disable_clipping(self):
        from qgis.core import QgsGeometry
        source, boundary = self.source_line(), self.boundary()
        with patch.object(QgsGeometry, "unaryUnion", return_value=QgsGeometry()):
            result = self.run_normalize(source, boundary)
        self.assertIn("裁剪边界合并", result["message"])
        self.assert_failed_cleanly(result, {source: source.read_bytes()})

    def test_boundary_transforms_into_input_crs(self):
        from qgis.core import QgsVectorLayer
        source = self.write("projected.geojson", [feature({
            "type": "LineString", "coordinates": [[-200000, 0], [200000, 0]],
        })], "EPSG:3857")
        boundary = self.write("wgs84.geojson", [feature(polygon([
            [[-1, -1], [1, -1], [1, 1], [-1, 1], [-1, -1]],
        ]))])
        result = self.run_normalize(source, boundary)
        self.assertTrue(result["success"], result)
        self.assertEqual(result["data"]["clip_source_crs"], "EPSG:4326")
        self.assertEqual(result["data"]["clip_crs"], "EPSG:3857")
        layer = QgsVectorLayer(result["data"]["path"], "result", "ogr")
        extent = next(layer.getFeatures()).geometry().boundingBox()
        self.assertAlmostEqual(extent.xMinimum(), -111319.49079327357, places=5)
        self.assertAlmostEqual(extent.xMaximum(), 111319.49079327357, places=5)

    def test_missing_boundary_crs_is_rejected(self):
        from qgis.core import QgsCoordinateReferenceSystem, QgsProject, QgsVectorFileWriter, QgsVectorLayer
        boundary = self.boundary()
        layer = QgsVectorLayer(str(boundary), "boundary", "ogr")
        layer.setCrs(QgsCoordinateReferenceSystem())
        target = self.root / "without-crs.shp"
        options = QgsVectorFileWriter.SaveVectorOptions()
        options.driverName = "ESRI Shapefile"
        write = QgsVectorFileWriter.writeAsVectorFormatV3(layer, str(target), QgsProject.instance().transformContext(), options)
        self.assertEqual(write[0], QgsVectorFileWriter.NoError, write)
        self.assertFalse(QgsVectorLayer(str(target), "without-crs", "ogr").crs().isValid())
        source = self.source_line()
        self.assert_failed_cleanly(self.run_normalize(source, target), {source: source.read_bytes(), target: target.read_bytes()})

    def test_transform_failure_is_rejected(self):
        from qgis.core import QgsGeometry
        source = self.write("projected.geojson", [feature({
            "type": "LineString", "coordinates": [[0, 0], [200000, 0]],
        })], "EPSG:3857")
        with patch.object(QgsGeometry, "transform", return_value=1):
            result = self.run_normalize(source, self.boundary())
        self.assertIn("坐标转换失败", result["message"])
        self.assert_failed_cleanly(result, {source: source.read_bytes()})

    def test_simplification_does_not_bridge_outside_a_concavity(self):
        from qgis.core import QgsVectorLayer
        boundary = self.write("concave.geojson", [feature(polygon([
            [[0, 0], [3, 0], [3, 1], [1, 1], [1, 3], [0, 3], [0, 0]],
        ]))])
        source = self.write("bent.geojson", [feature({
            "type": "LineString", "coordinates": [[.5, 2.5], [.5, .5], [2.5, .5]],
        })])
        result = self.run_normalize(source, boundary, simplify_tolerance_m=300000)
        self.assertTrue(result["success"], result)
        output_layer = QgsVectorLayer(result["data"]["path"], "output", "ogr")
        boundary_layer = QgsVectorLayer(str(boundary), "boundary", "ogr")
        geometry = next(output_layer.getFeatures()).geometry()
        clip = next(boundary_layer.getFeatures()).geometry()
        self.assertTrue(geometry.difference(clip).isEmpty(), geometry.asWkt())

    def test_no_clip_does_not_claim_clipping(self):
        result = self.run_normalize(self.source_line())
        self.assertTrue(result["success"], result)
        self.assertFalse(result["data"]["clip_applied"])
        self.assertNotIn("clip_path", result["data"])

    def test_existing_output_and_source_are_not_overwritten(self):
        source = self.source_line()
        before = source.read_bytes()
        result = self.run_normalize(source, self.boundary(), output=str(source))
        self.assertFalse(result["success"], result)
        self.assertEqual(source.read_bytes(), before)

    def test_scale_rules_merge_connected_same_ref_and_remove_short_line(self):
        source = self.write("segments.geojson", [feature({"type": "LineString", "coordinates": [[110+i*.01, 30], [110+(i+1)*.01, 30]]}, {"ref": "G4", "name:zh": "高速", "highway": "motorway"}) for i in range(50)] + [feature({"type": "LineString", "coordinates": [[111, 30], [111.00001, 30]]}, {"ref": "G5", "name:zh": "短段", "highway": "motorway"})])
        result = self.run_normalize(source, fields={"name": ["name:zh", "name"], "ref": ["ref"]}, merge_by="ref", min_length_m=100)
        self.assertTrue(result["success"], result)
        stats = result["data"]["generalization"]
        self.assertEqual(stats["input_features"], 51)
        self.assertEqual(stats["after_merge"], 2)
        self.assertEqual(stats["removed_small"], 1)
        self.assertEqual(result["data"]["count"], 1)

    def test_same_name_stations_merge_only_within_distance(self):
        source = self.write("stations.geojson", [feature({"type": "Point", "coordinates": [x,30]}, {"name": "测试站"}) for x in [110,110.001,110.01]])
        result = self.run_normalize(source, merge_by="name", merge_distance_m=300)
        self.assertTrue(result["success"], result)
        self.assertEqual(result["data"]["count"], 2)


if __name__ == "__main__":
    unittest.main()
