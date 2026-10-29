"""An accepted extraction is drawable without certifying its semantics."""
from autocarto.tests.support.resource_handles import load_handle, resolve_handle

import copy
import hashlib
import hmac
import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from autocarto.knowledge.extraction import build_extraction_contract
from autocarto.knowledge.handler import compile_marching_route_dataset_handler
from autocarto.tests.test_knowledge_geocoding_contract import graph_fixture


class AcceptedCompilationTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.source = "部队从甲地到乙地。"
        self.key = bytes(range(32))
        key_path = self.root / "data/knowledge_reviews/.signing_key"
        key_path.parent.mkdir(parents=True)
        key_path.write_text(self.key.hex())
        patcher = patch("autocarto.knowledge.review.get_autocarto_home", return_value=self.root)
        patcher.start()
        self.addCleanup(patcher.stop)

    def request(self, graph, *, signed=True, source_graph=None, adaptations=None, validate=False):
        params = {
            "source_text": self.source,
            "contract_id": build_extraction_contract(self.source)["contract_id"],
            "knowledge_json": graph, "validate_only": validate,
            "output_dir": str(self.root / "dataset"),
        }
        if signed:
            payload = {
                "version": 1, "review_id": "accepted", "revision": 2,
                "source_sha256": hashlib.sha256(self.source.encode()).hexdigest(),
                "graph_json": json.dumps(graph, ensure_ascii=False),
                "manual_records": [],
            }
            if source_graph is not None:
                payload["source_graph_json"] = json.dumps(source_graph, ensure_ascii=False)
            if adaptations is not None:
                payload["rendering_adaptations"] = adaptations
            encoded = json.dumps(payload, ensure_ascii=False)
            params["review_context"] = {
                "payload": encoded,
                "signature": hmac.new(self.key, encoded.encode(), hashlib.sha256).hexdigest(),
            }
        return compile_marching_route_dataset_handler({"route_style": load_handle("red-blue"), "symbols": load_handle("military"), **params})

    def test_content_findings_do_not_block_accepted_extraction(self):
        graph = graph_fixture()
        graph["spatial_entities"][0]["map_role"] = "background"
        graph["routes"][0].pop("confidence")
        graph["routes"][0]["evidence_refs"] = []
        graph["evidence"][0]["quote"] = "模型概括的文字，未逐字出现在原文。"
        before = copy.deepcopy(graph)
        preview = self.request(graph, validate=True)
        self.assertTrue(preview["data"]["validation"]["valid"], preview)
        result = self.request(graph)
        self.assertTrue(result["success"], result)
        self.assertEqual(result["data"]["table_counts"]["route_paths.csv"], 2)
        self.assertEqual(result["data"]["handoff"]["plan_data"], {
            "from": "session", "path": result["data"]["dataset_dir"], "kind": "dataset",
        })
        self.assertGreater(result["data"]["validation"]["warning_count"], 0)
        self.assertEqual(graph, before)
        saved = json.loads(Path(result["data"]["task_graph_file"]).read_text())
        self.assertEqual(saved, before)
        provenance = json.loads(Path(result["data"]["review_provenance_file"]).read_text())
        self.assertEqual(provenance["manual_records"], [])

    def test_unsigned_model_does_not_claim_acceptance(self):
        graph = graph_fixture()
        graph["spatial_entities"][0]["map_role"] = "background"
        result = self.request(graph, signed=False, validate=True)
        self.assertFalse(result["data"]["validation"]["valid"])

    def test_adapted_routes_keep_accepted_source_and_automatic_provenance(self):
        source_graph = graph_fixture()
        extra = copy.deepcopy(source_graph["routes"][0]["paths"][0])
        extra["path_id"] = "path2"
        extra["nodes"].reverse()
        source_graph["routes"][0]["paths"].append(extra)
        graph = copy.deepcopy(source_graph)
        route = graph["routes"][0]
        route["paths"] = [route["paths"][0]]
        extra_route = copy.deepcopy(route)
        extra_route["route_id"] = "route__path2"
        extra_route["paths"] = [extra]
        graph["routes"].append(extra_route)
        adaptations = [{"source_route_id": "route", "rendered_route_id": "route__path2", "path_id": "path2"}]
        result = self.request(graph, source_graph=source_graph, adaptations=adaptations)
        self.assertTrue(result["success"], result)
        directory = Path(result["data"]["dataset_dir"])
        self.assertEqual(json.loads((directory / "accepted_graph.json").read_text()), source_graph)
        self.assertEqual(result["data"]["table_counts"]["routes.csv"], 2)
        self.assertEqual(result["data"]["table_counts"]["route_paths.csv"], 4)
        provenance = json.loads((directory / "review_provenance.json").read_text())
        self.assertEqual(provenance["rendering_adaptations"], adaptations)
        self.assertEqual(provenance["manual_records"], [])

    def test_invalid_coordinate_cannot_be_reported_as_successful_geometry(self):
        graph = graph_fixture()
        graph["spatial_entities"][0]["lon"] = 999
        result = self.request(graph)
        self.assertFalse(result["success"])
        self.assertFalse((self.root / "dataset").exists())

    def test_isolated_movement_retains_its_point_without_inventing_a_line(self):
        original = graph_fixture()
        original["spatial_entities"] = original["spatial_entities"][:1]
        original["routes"][0]["paths"][0]["nodes"] = original["routes"][0]["paths"][0]["nodes"][:1]
        prepared = copy.deepcopy(original)
        prepared["routes"] = []
        result = self.request(prepared, source_graph=original, adaptations=[{
            "source_route_id": "route", "rendered_route_id": "", "path_id": "path",
            "source_role": "main", "rendered_role": "", "operation": "point_only",
        }])
        self.assertTrue(result["success"], result)
        counts = result["data"]["table_counts"]
        self.assertEqual((counts["routes.csv"], counts["route_paths.csv"]), (0, 0))
        self.assertEqual((counts["places.csv"], counts["points.csv"]), (1, 1))
        self.assertEqual(json.loads((self.root / "dataset/accepted_graph.json").read_text()), original)

    def test_missing_audit_references_do_not_block_geometry(self):
        graph = graph_fixture()
        graph["routes"][0]["evidence_refs"] = ["absent-evidence"]
        graph["routes"][0]["paths"][0]["actor_refs"] = ["absent-actor"]
        result = self.request(graph)
        self.assertTrue(result["success"], result)
        codes = {issue["code"] for issue in result["data"]["validation"]["issues"]}
        self.assertTrue({"dangling_actor_ref", "dangling_evidence_ref"}.issubset(codes))


if __name__ == "__main__":
    unittest.main()
