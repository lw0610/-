from autocarto.tests.support.resource_handles import load_handle, resolve_handle
import copy
import csv
import tempfile
import unittest
from pathlib import Path

from autocarto.knowledge.compiler import compile_task_knowledge
from autocarto.tests.test_knowledge_geocoding_contract import graph_fixture


class CalloutCompilationTest(unittest.TestCase):
    def test_legacy_titles_stay_in_source_but_never_reach_csv(self):
        graph = graph_fixture()
        with tempfile.TemporaryDirectory() as directory:
            image_path = Path(directory) / "callout.png"
            image_path.write_bytes(b"image asset is validated by the renderer")
            graph["callouts"] = [
                {"callout_id": "with_image", "anchor_ref_type": "spatial_entity", "anchor_ref": "a",
                 "title": "旧标题", "body": "完整正文", "image_enabled": True, "image_path": str(image_path)},
                {"callout_id": "no_image", "anchor_ref_type": "spatial_entity", "anchor_ref": "b",
                 "title": "另一个旧标题", "body": "纯文本正文"},
                {"callout_id": "explicit_text", "anchor_ref_type": "spatial_entity", "anchor_ref": "a",
                 "title": "", "body": "明确选择纯文本", "template": "text_only", "image_enabled": True, "image_path": str(image_path)},
            ]
            original = copy.deepcopy(graph)
            result = compile_task_knowledge(graph, Path(directory) / "dataset", route_style=load_handle("red-blue"), symbols=load_handle("military"))
            with (Path(result.dataset_dir) / "callouts.csv").open(encoding="utf-8-sig", newline="") as stream:
                rows = list(csv.DictReader(stream))
        self.assertEqual(graph, original)
        self.assertEqual([row["title"] for row in rows], ["", "", ""])
        self.assertEqual([row["template"] for row in rows], ["image_top_text_bottom", "text_only", "text_only"])
        self.assertEqual([row["leader_style"] for row in rows], ["double"] * 3)
        self.assertEqual([row["body"] for row in rows], [c["body"] for c in graph["callouts"]])


if __name__ == "__main__":
    unittest.main()
