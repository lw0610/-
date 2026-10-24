import concurrent.futures
import importlib.util
import json
import struct
import subprocess
import sys
import tempfile
import threading
import time
import unittest
import zlib
from pathlib import Path
from unittest import mock


SCRIPT_PATH = (
    Path(__file__).resolve().parents[1]
    / "tools"
    / "system"
    / "quality-feedback"
    / "scripts"
    / "quality_review.py"
)


def _load_module():
    spec = importlib.util.spec_from_file_location("quality_feedback_review", SCRIPT_PATH)
    module = importlib.util.module_from_spec(spec)
    assert spec.loader is not None
    spec.loader.exec_module(module)
    return module


def _png_chunk(chunk_type, payload):
    checksum = zlib.crc32(chunk_type + payload) & 0xFFFFFFFF
    return struct.pack(">I", len(payload)) + chunk_type + payload + struct.pack(">I", checksum)


def _png_bytes(width, height, *, dpi=None, color=(80, 120, 160, 255)):
    signature = b"\x89PNG\r\n\x1a\n"
    ihdr = struct.pack(">IIBBBBB", width, height, 8, 6, 0, 0, 0)
    chunks = [_png_chunk(b"IHDR", ihdr)]
    if dpi is not None:
        ppm = round(float(dpi) / 0.0254)
        chunks.append(_png_chunk(b"pHYs", struct.pack(">IIB", ppm, ppm, 1)))
    row = b"\x00" + bytes(color) * width
    chunks.append(_png_chunk(b"IDAT", zlib.compress(row * height, level=9)))
    chunks.append(_png_chunk(b"IEND", b""))
    return signature + b"".join(chunks)


def _png_with_raw_scanlines(width, height, raw_scanlines, *, dpi=None):
    signature = b"\x89PNG\r\n\x1a\n"
    ihdr = struct.pack(">IIBBBBB", width, height, 8, 6, 0, 0, 0)
    chunks = [_png_chunk(b"IHDR", ihdr)]
    if dpi is not None:
        ppm = round(float(dpi) / 0.0254)
        chunks.append(_png_chunk(b"pHYs", struct.pack(">IIB", ppm, ppm, 1)))
    chunks.append(_png_chunk(b"IDAT", zlib.compress(raw_scanlines)))
    chunks.append(_png_chunk(b"IEND", b""))
    return signature + b"".join(chunks)


class QualityFeedbackReviewTests(unittest.TestCase):
    def setUp(self):
        self.module = _load_module()
        self.temp_dir = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp_dir.cleanup)
        self.root = Path(self.temp_dir.name)
        self.original = self.root / "initial.png"
        self.original.write_bytes(_png_bytes(1024, 768, dpi=200))
        self.layout_item_ids = {
            "standard": {
                "main_map": "main_map",
                "title": "title",
                "legend": "legend",
            },
            "callouts": {},
        }
        self.layout_item_state = {
            "layout_name": "河南省行军路线综合图",
            "page_size_mm": {"width": 130.048, "height": 97.536},
            "items": {
                "main_map": {
                    "type": "map",
                    "rect_mm": {"x": 30.0, "y": 40.0, "width": 700.0, "height": 500.0},
                },
                "title": {
                    "type": "label",
                    "rect_mm": {"x": 250.0, "y": 12.0, "width": 340.0, "height": 20.0},
                },
                "legend": {
                    "type": "legend",
                    "rect_mm": {"x": 35.0, "y": 420.0, "width": 80.0, "height": 100.0},
                },
            },
        }

    def _snapshot_for(self, *, state=None, item_ids=None, schema_version=None):
        snapshot = self.root / f"layout-snapshot-{time.time_ns()}.json"
        snapshot.write_text(
            json.dumps(
                {
                    "schema_version": schema_version
                    or "quality-feedback-layout-snapshot/v1",
                    "snapshot_id": f"test-{time.time_ns()}",
                    "layout_name": "河南省行军路线综合图",
                    "layout_item_ids": item_ids or self.layout_item_ids,
                    "layout_item_state": state or self.layout_item_state,
                },
                ensure_ascii=False,
            ),
            encoding="utf-8",
        )
        return snapshot

    def _prepare(self):
        return self.module.prepare_review(
            {
                "original_image_path": str(self.original),
                "layout_snapshot_path": str(self._snapshot_for()),
                "task_requirement": "制作河南省行军路线专题图，并启用质量反馈",
                "locked_content": ["所有文字", "路线拓扑", "地图范围", "图例语义"],
            }
        )

    @staticmethod
    def _review_params(prepared, final, applied=None):
        return {
            "review_manifest_path": prepared["review_manifest_path"],
            "final_image_path": str(final),
            "applied_suggestions": applied or [],
        }

    def test_prepare_creates_v3_original_final_manifest_and_one_media_image(self):
        output = self._prepare()
        protected = Path(output["original_image_path"])
        manifest_path = Path(output["review_manifest_path"])
        self.assertTrue(protected.is_file())
        self.assertTrue(manifest_path.is_file())
        self.assertFalse(Path(output["reserved_final_image_path"]).exists())
        self.assertEqual(protected.read_bytes(), self.original.read_bytes())
        self.assertEqual(len(output[self.module.MEDIA_KEY]), 1)
        self.assertEqual(output[self.module.MEDIA_KEY][0]["path"], str(protected))
        self.assertNotIn("candidate_image_path", output)

        manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
        self.assertEqual(manifest["schema_version"], "quality-feedback/v3")
        self.assertEqual(manifest["status"], "initial_ready")
        self.assertEqual(set(manifest["images"]), {"original", "final"})
        self.assertEqual(manifest["review_context"]["layout_item_ids"], self.layout_item_ids)
        self.assertEqual(manifest["review_context"]["layout_item_state"], self.layout_item_state)
        self.assertEqual(manifest["review_context"]["initial_export_dpi"], 200)
        self.assertEqual(
            manifest["review_context"]["layout_snapshot"]["path"],
            output["layout_snapshot_path"],
        )
        self.assertNotIn("candidate", json.dumps(manifest))

    def test_prepare_requires_layout_snapshot_path(self):
        base = {
            "original_image_path": str(self.original),
            "task_requirement": "制图",
            "locked_content": ["文字"],
        }
        with self.assertRaisesRegex(ValueError, "layout_snapshot_path"):
            self.module.prepare_review(base)

    def test_prepare_validates_snapshot_page_and_each_handed_off_item_state(self):
        cases = []

        missing_page = json.loads(json.dumps(self.layout_item_state))
        missing_page.pop("page_size_mm")
        cases.append((missing_page, "page_size_mm"))

        invalid_page = json.loads(json.dumps(self.layout_item_state))
        invalid_page["page_size_mm"]["width"] = 0
        cases.append((invalid_page, "width 必须大于 0"))

        missing_type = json.loads(json.dumps(self.layout_item_state))
        missing_type["items"]["title"].pop("type")
        cases.append((missing_type, "title.type"))

        missing_rect = json.loads(json.dumps(self.layout_item_state))
        missing_rect["items"]["legend"].pop("rect_mm")
        cases.append((missing_rect, "legend 缺少 rect_mm"))

        invalid_rect = json.loads(json.dumps(self.layout_item_state))
        invalid_rect["items"]["main_map"]["rect_mm"]["height"] = 0
        cases.append((invalid_rect, "main_map.rect_mm.height 必须大于 0"))

        for state, pattern in cases:
            with self.subTest(pattern=pattern), self.assertRaisesRegex(ValueError, pattern):
                self.module.prepare_review(
                    {
                        "original_image_path": str(self.original),
                        "layout_snapshot_path": str(self._snapshot_for(state=state)),
                        "task_requirement": "制图",
                        "locked_content": ["文字"],
                    }
                )

    def test_prepare_rejects_unsupported_snapshot_and_image_page_mismatch(self):
        with self.assertRaisesRegex(ValueError, "版本不受支持"):
            self.module.prepare_review(
                {
                    "original_image_path": str(self.original),
                    "layout_snapshot_path": str(
                        self._snapshot_for(schema_version="unsupported/v1")
                    ),
                    "task_requirement": "制图",
                    "locked_content": ["文字"],
                }
            )

        mismatched_state = json.loads(json.dumps(self.layout_item_state))
        mismatched_state["page_size_mm"] = {"width": 100.0, "height": 100.0}
        with self.assertRaisesRegex(ValueError, "宽高比"):
            self.module.prepare_review(
                {
                    "original_image_path": str(self.original),
                    "layout_snapshot_path": str(
                        self._snapshot_for(state=mismatched_state)
                    ),
                    "task_requirement": "制图",
                    "locked_content": ["文字"],
                }
            )

    def test_final_review_loads_only_original_and_final(self):
        prepared = self._prepare()
        final = Path(prepared["reserved_final_image_path"])
        final.write_bytes(_png_bytes(1024, 768, dpi=200, color=(20, 40, 60, 255)))
        applied = [
            {
                "suggestion_id": "QF-001",
                "status": "applied",
                "tool_calls": [{"tool_name": "layout_move_item"}],
            }
        ]
        output = self.module.load_review_images(self._review_params(prepared, final, applied))
        self.assertEqual(len(output[self.module.MEDIA_KEY]), 2)
        self.assertIn("初版", output[self.module.MEDIA_KEY][0]["label"])
        self.assertIn("终稿", output[self.module.MEDIA_KEY][1]["label"])
        self.assertEqual(output["review_context"]["applied_suggestions"], applied)
        self.assertNotIn("candidate", json.dumps(output, ensure_ascii=False))

        manifest = json.loads(Path(prepared["review_manifest_path"]).read_text(encoding="utf-8"))
        self.assertEqual(manifest["status"], "final_ready")
        self.assertEqual(
            manifest["images"]["final"]["metadata"]["sha256"],
            output["final_image"]["sha256"],
        )

    def test_no_visual_change_final_is_valid_and_idempotent(self):
        prepared = self._prepare()
        final = Path(prepared["reserved_final_image_path"])
        final.write_bytes(self.original.read_bytes())
        params = self._review_params(prepared, final)
        first = self.module.load_review_images(params)
        second = self.module.load_review_images(params)
        self.assertEqual(first["final_image"]["sha256"], second["final_image"]["sha256"])

    def test_bound_final_cannot_be_replaced_or_rebound_to_other_suggestions(self):
        prepared = self._prepare()
        final = Path(prepared["reserved_final_image_path"])
        final.write_bytes(_png_bytes(1024, 768, dpi=200, color=(1, 2, 3, 255)))
        first_suggestions = [{"suggestion_id": "QF-001", "status": "applied"}]
        self.module.load_review_images(self._review_params(prepared, final, first_suggestions))

        with self.assertRaisesRegex(ValueError, "另一组已执行建议"):
            self.module.load_review_images(self._review_params(prepared, final, []))
        final.write_bytes(_png_bytes(1024, 768, dpi=200, color=(4, 5, 6, 255)))
        with self.assertRaisesRegex(ValueError, "另一份终稿"):
            self.module.load_review_images(self._review_params(prepared, final, first_suggestions))

    def test_concurrent_binding_accepts_only_one_suggestion_set(self):
        prepared = self._prepare()
        final = Path(prepared["reserved_final_image_path"])
        final.write_bytes(_png_bytes(1024, 768, dpi=200, color=(8, 9, 10, 255)))
        variants = [
            [{"suggestion_id": "QF-001", "status": "applied"}],
            [{"suggestion_id": "QF-002", "status": "applied"}],
        ]
        barrier = threading.Barrier(2)
        original_write = self.module._atomic_write_json

        def slow_write(*args, **kwargs):
            time.sleep(0.03)
            return original_write(*args, **kwargs)

        def bind(applied):
            barrier.wait()
            try:
                self.module.load_review_images(self._review_params(prepared, final, applied))
                return "success"
            except ValueError as exc:
                return str(exc)

        with mock.patch.object(self.module, "_atomic_write_json", side_effect=slow_write), concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
            outcomes = list(pool.map(bind, variants))
        self.assertEqual(outcomes.count("success"), 1)
        self.assertEqual(sum("另一组已执行建议" in value for value in outcomes), 1)

    def test_final_review_rejects_wrong_path_resolution_dpi_and_original_drift(self):
        for failure in ("path", "resolution", "dpi", "drift"):
            with self.subTest(failure=failure):
                prepared = self._prepare()
                final = Path(prepared["reserved_final_image_path"])
                target = final
                if failure == "path":
                    target = self.root / f"wrong-{time.time_ns()}.png"
                    target.write_bytes(_png_bytes(1024, 768, dpi=200))
                    pattern = "预留路径"
                elif failure == "resolution":
                    final.write_bytes(_png_bytes(800, 600, dpi=200))
                    pattern = "像素尺寸"
                elif failure == "dpi":
                    final.write_bytes(_png_bytes(1024, 768, dpi=96))
                    pattern = "DPI"
                else:
                    Path(prepared["original_image_path"]).write_bytes(_png_bytes(1024, 768, dpi=200, color=(9, 9, 9, 255)))
                    final.write_bytes(_png_bytes(1024, 768, dpi=200))
                    pattern = "覆盖或修改"
                with self.assertRaisesRegex(ValueError, pattern):
                    self.module.load_review_images(self._review_params(prepared, target))

    def test_symlinked_final_is_rejected(self):
        prepared = self._prepare()
        external = self.root / "external.png"
        external.write_bytes(_png_bytes(1024, 768, dpi=200))
        final = Path(prepared["reserved_final_image_path"])
        final.symlink_to(external)
        with self.assertRaisesRegex(ValueError, "符号链接"):
            self.module.load_review_images(self._review_params(prepared, final))

    def test_corrupt_png_is_rejected(self):
        valid = _png_bytes(32, 24)
        with self.assertRaisesRegex(ValueError, "PNG"):
            self.module._inspect_image_bytes(valid[:-8], "image/png")
        corrupt = bytearray(valid)
        idat = corrupt.index(b"IDAT")
        corrupt[idat + 4] ^= 0x01
        with self.assertRaisesRegex(ValueError, "CRC"):
            self.module._inspect_image_bytes(bytes(corrupt), "image/png")

    def test_png_scanline_length_filter_and_pixel_budget_are_enforced(self):
        wrong_length = _png_with_raw_scanlines(2, 1, b"\x00" + b"\x00" * 7)
        with self.assertRaisesRegex(ValueError, "解压长度"):
            self.module._inspect_image_bytes(wrong_length, "image/png")

        invalid_filter = _png_with_raw_scanlines(2, 1, b"\x05" + b"\x00" * 8)
        with self.assertRaisesRegex(ValueError, "过滤器"):
            self.module._inspect_image_bytes(invalid_filter, "image/png")

        oversized = _png_with_raw_scanlines(4001, 4000, b"\x00")
        with self.assertRaisesRegex(ValueError, "1600 万像素"):
            self.module._inspect_image_bytes(oversized, "image/png")

    def test_cli_contract_emits_one_json_document(self):
        payload = {
            "original_image_path": str(self.original),
            "layout_snapshot_path": str(self._snapshot_for()),
            "task_requirement": "制图质量检查",
            "locked_content": ["文字", "地图范围"],
        }
        completed = subprocess.run(
            [sys.executable, str(SCRIPT_PATH), "prepare"],
            input=json.dumps(payload),
            text=True,
            capture_output=True,
            check=True,
        )
        result = json.loads(completed.stdout)
        self.assertTrue(result["success"])
        self.assertEqual(result["error"], "")
        self.assertEqual(len(result["output"][self.module.MEDIA_KEY]), 1)
        self.assertEqual(completed.stderr, "")

    def test_quality_feedback_workflow_contract_is_wired_without_image2(self):
        workspace = SCRIPT_PATH.parents[5]
        prepare_yaml = (workspace / "autocarto/tools/system/quality-feedback/prepare_quality_review.yaml").read_text(encoding="utf-8")
        load_yaml = (workspace / "autocarto/tools/system/quality-feedback/load_review_images.yaml").read_text(encoding="utf-8")
        layout_state_yaml = (workspace / "autocarto/tools/system/print-layout/layout_get_items_properties.yaml").read_text(encoding="utf-8")
        move_callout_yaml = (workspace / "autocarto/tools/system/print-layout/layout_move_callout.yaml").read_text(encoding="utf-8")
        self.assertIn("layout_snapshot_path", prepare_yaml)
        self.assertIn("original_image_path", prepare_yaml)
        self.assertNotIn("initial_export_dpi", prepare_yaml)
        self.assertIn("layout_item_ids", layout_state_yaml)
        self.assertIn("required: false", layout_state_yaml)
        self.assertIn("layout_snapshot_path", layout_state_yaml)
        self.assertIn("name: layout_move_callout", move_callout_yaml)
        self.assertIn("callout_item_ids", move_callout_yaml)
        self.assertIn("container_item_id", move_callout_yaml)
        self.assertIn("review_manifest_path", load_yaml)
        self.assertFalse((workspace / "autocarto/tools/system/quality-feedback/generate_image2_candidate.yaml").exists())
        self.assertFalse((workspace / "autocarto/tools/system/quality-feedback/scripts/image2_candidate.py").exists())


    def test_prepare_preserves_legal_anchors_from_verified_snapshot(self):
        anchor = {"to": "main_map", "at": "top_center", "placement": "outside"}
        self.layout_item_state["items"]["title"].update(anchor=anchor, font_locked=True)
        output = self._prepare()
        self.assertEqual(output["review_context"]["legal_anchors"], {"title": anchor})
        manifest = json.loads(Path(output["review_manifest_path"]).read_text())
        self.assertEqual(manifest["review_context"]["legal_anchors"], {"title": anchor})


if __name__ == "__main__":
    unittest.main()
