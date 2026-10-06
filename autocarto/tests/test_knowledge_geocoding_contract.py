import csv
import json
import tempfile
import unittest
from pathlib import Path

from autocarto.knowledge.compiler import compile_task_knowledge
from autocarto.knowledge.schema import normalize_task_knowledge
from autocarto.knowledge.validator import validate_task_knowledge


def graph_fixture():
    graph = {
        "schema_version": "1.0",
        "task": {
            "task_id": "route_test", "title": "路线", "map_type": "marching_route",
            "input_crs": "EPSG:4326",
        },
        "spatial_entities": [
            {
                "entity_id": "a", "name": "甲地", "entity_kind": "place", "factual": True,
                "map_role": "route_node", "evidence_refs": ["e1"], "lon": 109.8, "lat": 33.5,
                "coordinate_source": "gazetteer",
                "geocoding": {
                    "provider": "tianditu", "status": "resolved", "original_name": "甲地",
                    "query": "某县甲地", "modern_name": "甲地", "administrative_hint": "某县",
                    "candidates": [{"name": "甲地", "address": "某县甲地", "lon": 109.8, "lat": 33.5, "source_id": "poi-a"}],
                },
            },
            {
                "entity_id": "b", "name": "乙地", "entity_kind": "place", "factual": True,
                "map_role": "route_node", "evidence_refs": ["e1"], "lon": 110.1, "lat": 34.0,
                "coordinate_source": "gazetteer", "geocoding": {"status": "confirmed"},
            },
        ],
        "routes": [{
            "route_id": "route", "name": "甲至乙", "topology": "standard", "side": "friendly",
            "confidence": 1, "evidence_refs": ["e1"],
            "paths": [{"path_id": "path", "role": "main", "direction": "forward", "nodes": [
                {"ref_type": "spatial_entity", "ref_id": "a"},
                {"ref_type": "spatial_entity", "ref_id": "b"},
            ]}],
        }],
        "evidence": [{"evidence_id": "e1", "quote": "部队从甲地到乙地。"}],
    }
    return normalize_task_knowledge(graph)


class KnowledgeGeocodingContractTest(unittest.TestCase):
    def test_gazetteer_does_not_require_invented_confidence(self):
        graph = graph_fixture()
        report = validate_task_knowledge(graph)
        self.assertTrue(report.is_valid, report.to_dict())
        self.assertNotIn("coordinate_confidence", graph["spatial_entities"][0])
        graph["spatial_entities"][0]["coordinate_confidence"] = 1.5
        self.assertFalse(validate_task_knowledge(graph).is_valid)

    def test_literal_source_coordinates_compile_without_invented_confidence(self):
        graph = graph_fixture()
        source = "甲地东经109.8度、北纬33.5度，部队从甲地到乙地。"
        graph["evidence"][0]["quote"] = source
        entity = graph["spatial_entities"][0]
        entity["coordinate_source"] = "source_text"
        entity.pop("geocoding")
        report = validate_task_knowledge(graph, source_text=source)
        self.assertTrue(report.is_valid, report.to_dict())
        with tempfile.TemporaryDirectory() as temp:
            result = compile_task_knowledge(graph, Path(temp) / "dataset", source_text=source)
            saved = json.loads(Path(result.task_graph_file).read_text())
            saved_entity = saved["spatial_entities"][0]
            self.assertEqual(saved_entity["coordinate_source"], "source_text")
            self.assertNotIn("coordinate_confidence", saved_entity)
        entity["coordinate_confidence"] = 1.5
        self.assertFalse(validate_task_knowledge(graph).is_valid)
        entity.pop("coordinate_confidence")
        entity["coordinate_source"] = "llm_estimated"
        report = validate_task_knowledge(graph)
        self.assertFalse(report.is_valid)
        self.assertTrue(any(issue.code == "invalid_confidence" for issue in report.errors))

    def test_lookup_metadata_and_coordinates_survive_compile(self):
        graph = graph_fixture()
        original = json.loads(json.dumps(graph))
        with tempfile.TemporaryDirectory() as temp:
            result = compile_task_knowledge(graph, Path(temp) / "dataset")
            saved = json.loads(Path(result.task_graph_file).read_text())
            self.assertEqual(saved["spatial_entities"], graph["spatial_entities"])
            with (Path(result.dataset_dir) / "places.csv").open(encoding="utf-8-sig", newline="") as stream:
                rows = list(csv.DictReader(stream))
            self.assertEqual(
                [(row["place_id"], float(row["x"]), float(row["y"])) for row in rows],
                [("a", 109.8, 33.5), ("b", 110.1, 34.0)],
            )
        self.assertEqual(graph, original)

    def test_unresolved_candidates_remain_a_valid_draft_without_coordinates(self):
        graph = graph_fixture()
        entity = graph["spatial_entities"][0]
        for key in ("lon", "lat", "coordinate_source"):
            entity.pop(key)
        entity["geocoding"]["status"] = "ambiguous"
        report = validate_task_knowledge(graph)
        self.assertTrue(report.is_valid, report.to_dict())
        self.assertIn("coordinate_pending", [issue.code for issue in report.warnings])

    def test_legacy_model_estimates_are_still_readable(self):
        graph = graph_fixture()
        entity = graph["spatial_entities"][0]
        entity["coordinate_source"] = "llm_estimated"
        entity["coordinate_confidence"] = 0.7
        self.assertTrue(validate_task_knowledge(graph).is_valid)

    def test_api_point_and_estimated_region_and_direction_compile_without_review(self):
        graph = graph_fixture()
        source = "部队从甲地途经伏牛山区，抵达西安南部。"
        graph["evidence"][0]["quote"] = source
        region = graph["spatial_entities"][1]
        region.update({
            "name": "伏牛山区", "entity_kind": "region", "lon": 111.7, "lat": 33.7,
            "coordinate_source": "llm_estimated", "coordinate_confidence": 0.35,
            "geocoding": {"status": "estimated", "provider": "llm", "reason": "根据原文路线及区域范围估算代表位置"},
        })
        graph["spatial_entities"].append({
            "entity_id": "c", "name": "西安南部", "entity_kind": "place", "factual": True,
            "map_role": "route_node", "evidence_refs": ["e1"], "lon": 108.94, "lat": 34.14,
            "coordinate_source": "llm_estimated", "coordinate_confidence": 0.4,
            "geocoding": {"status": "estimated", "provider": "llm", "reason": "根据方位描述估算位置"},
        })
        graph["routes"][0]["paths"][0]["nodes"].append({"ref_type": "spatial_entity", "ref_id": "c"})
        report = validate_task_knowledge(graph, source_text=source)
        self.assertTrue(report.is_valid, report.to_dict())
        estimate_warnings = [issue for issue in report.warnings if issue.code == "low_coordinate_confidence"]
        self.assertEqual(len(estimate_warnings), 2)
        self.assertTrue(all("按估算位置参与制图" in issue.message for issue in estimate_warnings))
        with tempfile.TemporaryDirectory() as temp:
            # No signed review context or per-place manual approval is required.
            result = compile_task_knowledge(graph, Path(temp) / "dataset", source_text=source)
            saved = json.loads(Path(result.task_graph_file).read_text())
            self.assertEqual(saved["spatial_entities"], graph["spatial_entities"])
            self.assertEqual(saved["routes"], graph["routes"])
            with (Path(result.dataset_dir) / "places.csv").open(encoding="utf-8-sig", newline="") as stream:
                rows = list(csv.DictReader(stream))
            self.assertEqual(
                [(row["place_id"], float(row["x"]), float(row["y"])) for row in rows],
                [("a", 109.8, 33.5), ("b", 111.7, 33.7), ("c", 108.94, 34.14)],
            )


if __name__ == "__main__":
    unittest.main()
