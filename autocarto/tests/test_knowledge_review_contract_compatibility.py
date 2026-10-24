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


def contract_id_for(schema, source):
    canonical = json.dumps(schema, ensure_ascii=False, sort_keys=True, separators=(",", ":"))
    return "tkg-v1-sha256:" + hashlib.sha256((canonical + "\0" + source).encode()).hexdigest()


class KnowledgeReviewContractCompatibilityTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.key = bytes(range(32))
        key_path = self.root / "data" / "knowledge_reviews" / ".signing_key"
        key_path.parent.mkdir(parents=True)
        key_path.write_text(self.key.hex(), encoding="ascii")
        patcher = patch("autocarto.knowledge.review.get_autocarto_home", return_value=self.root)
        patcher.start()
        self.addCleanup(patcher.stop)
        self.source = "部队从甲地到乙地。"
        self.graph = graph_fixture()
        self.current = build_extraction_contract(self.source, compact=True)
        self.previous_schema = copy.deepcopy(self.current["json_schema"])
        del self.previous_schema["properties"]["spatial_entities"]["items"]["properties"]["geocoding"]
        self.previous_id = contract_id_for(self.previous_schema, self.source)

    def token(self, graph=None, source=None):
        graph = self.graph if graph is None else graph
        source = self.source if source is None else source
        payload = json.dumps({
            "version": 1, "review_id": "saved-review", "revision": 2,
            "source_sha256": hashlib.sha256(source.encode()).hexdigest(),
            "graph_json": json.dumps(graph, ensure_ascii=False, separators=(",", ":")),
            "manual_records": [],
        }, ensure_ascii=False, separators=(",", ":"))
        return {
            "payload": payload,
            "signature": hmac.new(self.key, payload.encode(), hashlib.sha256).hexdigest(),
        }

    def request(self, **overrides):
        params = {
            "source_text": self.source, "contract_id": self.previous_id,
            "knowledge_json": self.graph, "review_context": self.token(), "validate_only": True,
        }
        params.update(overrides)
        return compile_marching_route_dataset_handler({"route_style": load_handle("red-blue"), "symbols": load_handle("military"), **params})

    def test_signed_previous_review_validates_with_lookup_metadata(self):
        original = copy.deepcopy(self.graph)
        result = self.request()
        self.assertTrue(result["success"], result)
        self.assertTrue(result["data"]["validation"]["valid"], result)
        self.assertEqual(self.graph, original)

    def test_signed_previous_review_compiles_enriched_graph(self):
        result = self.request(validate_only=False, output_dir=str(self.root / "dataset"))
        self.assertTrue(result["success"], result)
        saved = json.loads(Path(result["data"]["task_graph_file"]).read_text())
        self.assertEqual(saved["spatial_entities"], self.graph["spatial_entities"])
        self.assertTrue(Path(result["data"]["review_provenance_file"]).is_file())

    def test_previous_contract_requires_authenticated_review(self):
        params = {
            "source_text": self.source, "contract_id": self.previous_id,
            "knowledge_json": self.graph, "validate_only": True,
        }
        result = compile_marching_route_dataset_handler({"route_style": load_handle("red-blue"), "symbols": load_handle("military"), **params})
        self.assertFalse(result["success"])
        self.assertEqual(result["error_type"], "task_knowledge_contract_mismatch")
        forged = self.token()
        forged["signature"] = "0" * 64
        result = self.request(review_context=forged)
        self.assertFalse(result["success"])
        self.assertEqual(result["error_type"], "task_knowledge_review_invalid")

    def test_other_schema_and_arbitrary_hashes_are_rejected(self):
        incompatible_schema = copy.deepcopy(self.previous_schema)
        incompatible_schema["properties"]["task"]["properties"]["input_crs"]["const"] = "EPSG:4490"
        for contract_id in ("", "tkg-v1-sha256:" + "0" * 64, contract_id_for(incompatible_schema, self.source)):
            with self.subTest(contract_id=contract_id):
                result = self.request(contract_id=contract_id)
                self.assertFalse(result["success"])
                self.assertEqual(result["error_type"], "task_knowledge_contract_mismatch")

    def test_signature_still_binds_source_and_full_graph(self):
        changed_graph = copy.deepcopy(self.graph)
        changed_graph["spatial_entities"][0]["lon"] = 111.3
        result = self.request(knowledge_json=changed_graph)
        self.assertFalse(result["success"])
        self.assertEqual(result["error_type"], "task_knowledge_review_invalid")
        changed_source = self.source + "次日返回。"
        result = self.request(
            source_text=changed_source,
            contract_id=contract_id_for(self.previous_schema, changed_source),
        )
        self.assertFalse(result["success"])
        self.assertEqual(result["error_type"], "task_knowledge_review_invalid")

    def test_migration_keeps_current_schema_validation(self):
        invalid_graph = copy.deepcopy(self.graph)
        invalid_graph["spatial_entities"][0]["lon"] = 999
        result = self.request(knowledge_json=invalid_graph, review_context=self.token(invalid_graph))
        self.assertTrue(result["success"], result)
        self.assertFalse(result["data"]["validation"]["valid"])
        self.assertTrue(any("lon" in issue["path"] for issue in result["data"]["validation"]["errors"]))
        result = self.request(
            knowledge_json=invalid_graph, review_context=self.token(invalid_graph),
            validate_only=False, output_dir=str(self.root / "invalid-dataset"),
        )
        self.assertFalse(result["success"])
        self.assertFalse((self.root / "invalid-dataset").exists())

    def test_current_contract_still_works_without_review_token(self):
        result = compile_marching_route_dataset_handler({"route_style": load_handle("red-blue"), "symbols": load_handle("military"), **{
            "source_text": self.source, "contract_id": self.current["contract_id"],
            "knowledge_json": self.graph, "validate_only": True,
        }})
        self.assertTrue(result["success"], result)
        self.assertTrue(result["data"]["validation"]["valid"], result)


if __name__ == "__main__":
    unittest.main()
