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

        config_paths = [
            workspace / "autocarto/config/agent_configs/default/agent.yaml",
            workspace / "carto_agent/defaults/agent_configs/default/agent.yaml",
            workspace / "autocarto_data/config/agent_configs/default/agent.yaml",
        ]
        for path in config_paths:
            text = path.read_text(encoding="utf-8")
            self.assertIn("provider: qwen", text, path)
            self.assertIn("prepare_quality_review", text, path)
            self.assertIn("initial_review", text, path)
            self.assertIn("第一步必须调用 quality-feedback skill", text, path)
            self.assertIn("元素位于 main_map 内或与其矩形相交本身不构成压盖", text, path)
            self.assertIn("只有能说明“哪个标注遮住了哪个专题保护内容”才判定有问题", text, path)
            self.assertIn("禁止打分或用“分数尚可”代替问题判断", text, path)
            self.assertIn("均不能作为接受真实专题压盖的理由", text, path)
            self.assertIn("低优先级底图", text, path)
            self.assertIn("不得为了寻找纯空白区域", text, path)
            self.assertIn("suggestions=[] 只允许八项全部 has_issue=false", text, path)
            self.assertIn("候选方案，不是命令", text, path)
            self.assertIn("evidence、desired_result", text, path)
            self.assertIn("禁止输出 layout_set_label_style 或任何 font_size 修改", text, path)
            self.assertIn("cartographer 对 executable=true 建议拥有最终执行判断权", text, path)
            self.assertIn("layout_move_callout", text, path)
            self.assertIn("created_item_ids 原样", text, path)
            self.assertIn("禁止建议修改任何 font_size", text, path)
            self.assertIn("禁止声称“没有整组移动工具”", text, path)
            self.assertIn("质量反馈给出的目标框左上角绝对 x/y", text, path)
            self.assertIn("executable=false 重新解释成“评估并尝试”", text, path)
            self.assertIn("不得将候选建议改写成“必须执行”“按顺序执行”", text, path)
            self.assertIn("layout_snapshot_path", text, path)
            self.assertIn("A1=140", text, path)
            self.assertIn("orchestrator 严禁直接调用", text, path)
            self.assertIn("禁止要求“各维度评分”", text, path)
            self.assertIn("该交接自相矛盾、不得进入 cartographer", text, path)
            self.assertIn("由你负责调用 prepare_quality_review", text, path)
            self.assertNotIn("layout_name=artifacts.layout", text, path)
            self.assertNotIn("generate_image2_candidate", text, path)
            self.assertNotIn("candidate_review", text, path)
            self.assertNotIn("当前没有安全整组移动能力时", text, path)
            self.assertNotIn("图文标注整组问题在没有安全整组移动工具时", text, path)

        source_print = (workspace / "autocarto/skills/system/print-layout-marching-route/SKILL.md").read_text(encoding="utf-8")
        embedded_print = (workspace / "carto_agent/defaults/skills/print-layout-marching-route/SKILL.md").read_text(encoding="utf-8")
        source_flow = (workspace / "autocarto/skills/system/marching-route-map-orchestration/SKILL.md").read_text(encoding="utf-8")
        embedded_flow = (workspace / "carto_agent/defaults/skills/marching-route-map-orchestration/SKILL.md").read_text(encoding="utf-8")
        quality_skill = (workspace / "autocarto/skills/system/quality-feedback/SKILL.md").read_text(encoding="utf-8")
        self.assertIn("layout_get_items_properties", source_print)
        self.assertIn("data.layout_snapshot_path", source_print)
        self.assertIn("## 质量反馈修改约束", source_print)
        self.assertIn("候选建议，不是强制命令", source_print)
        self.assertIn("left/top/right/bottom", source_print)
        self.assertIn("不得分别移动子元素", source_print)
        self.assertIn("layout_move_callout", source_print)
        self.assertIn("data.anchor_preserved", source_print)
        self.assertIn("纸张字号表是质量修改阶段的锁定规则", source_print)
        self.assertIn("不得以“看不到初版", source_print)
        self.assertEqual(source_print.replace("\r\n", "\n"), embedded_print.replace("\r\n", "\n"))
        self.assertIn("initial_review", source_flow)
        self.assertIn("所有文字原文", source_flow)
        self.assertIn("哪个标注遮住了哪个专题保护内容", source_flow)
        self.assertIn("禁止打分或以“分数尚可”", source_flow)
        self.assertIn("不能作为接受真实专题压盖的理由", source_flow)
        self.assertIn("低优先级县级地名和其他 background 要素允许被图文标注覆盖", source_flow)
        self.assertIn("不得为寻找纯空白区域", source_flow)
        self.assertIn("该交接自相矛盾", source_flow)
        self.assertIn("必须原样传递完整", source_flow)
        self.assertIn("不是强制命令", source_flow)
        self.assertIn("禁止修改任何 `font_size`", source_flow)
        self.assertIn("不得报告“缺少整组移动", source_flow)
        self.assertNotIn("candidate_image_path", source_flow)
        self.assertEqual(source_flow.replace("\r\n", "\n"), embedded_flow.replace("\r\n", "\n"))
        self.assertIn("adjustment=null", quality_skill)
        self.assertIn("专题内容压盖硬规则", quality_skill)
        self.assertIn("专题图中很常见", quality_skill)
        self.assertIn("suggestions=[]` 只允许", quality_skill)
        self.assertIn("evidence", quality_skill)
        self.assertIn("desired_result", quality_skill)
        self.assertIn("元素的矩形与 `main_map` 相交，不等于它压盖了地图内容", quality_skill)
        self.assertIn("必须先检查真正影响地图信息的压盖", quality_skill)
        self.assertIn("低优先级底图", quality_skill)
        self.assertIn("可以覆盖这些内容", quality_skill)
        self.assertIn("已专题符号化的节点", quality_skill)
        self.assertIn("优先采用距离原位置最近的可用位置", quality_skill)
        self.assertIn("不得为了寻找纯空白区域", quality_skill)
        self.assertNotIn("移动到安全空白区", quality_skill)
        self.assertIn("layout_move_callout", quality_skill)
        self.assertIn("固定地图锚点", quality_skill)
        self.assertIn("质量反馈不得建议修改任何元素的字号", quality_skill)
        self.assertIn("`layout_set_label_style` 不属于质量反馈可建议的工具", quality_skill)
        self.assertIn("禁止在 `unavailable_reason` 中写", quality_skill)
        self.assertIn("不能留给看不到图片的", quality_skill)
        self.assertIn("仅用 `layout_resize_item` 放大综述框", quality_skill)
        self.assertIn("不得仅因终稿坐标等于某条建议就判定", quality_skill)
        self.assertNotIn("layout_set_north_arrow_style`：", quality_skill)
        for direction in ("压盖", "整体布局", "对齐与间距", "视觉层级", "文字排版", "配色", "地图可读性", "风格一致性"):
            self.assertIn(direction, quality_skill)


if __name__ == "__main__":
    unittest.main()
