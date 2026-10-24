import ast
import unittest
from pathlib import Path

from autocarto.tools.system._shared.callout_text_layout import (
    font_metrics_mm_per_pixel,
    wrap_chinese_text_to_mm,
)


class _FakeFontMetrics:
    def __init__(self, *, dpi=72.0, char_width_px=4.8, line_spacing_px=6.0):
        self._dpi = dpi
        self._char_width_px = char_width_px
        self._line_spacing_px = line_spacing_px

    def fontDpi(self):
        return self._dpi

    def horizontalAdvance(self, text):
        return len(str(text)) * self._char_width_px

    def lineSpacing(self):
        return self._line_spacing_px


class CalloutTextLayoutTestCase(unittest.TestCase):
    def test_callout_precondition_failures_return_info_and_message(self):
        implementation = (
            Path(__file__).resolve().parents[1]
            / "tools"
            / "system"
            / "_shared"
            / "callouts_impl.py"
        )
        tree = ast.parse(implementation.read_text(encoding="utf-8"))
        callout_function = next(
            node
            for node in tree.body
            if isinstance(node, ast.FunctionDef)
            and node.name == "_cartography_add_callouts_impl"
        )

        precondition_returns = []
        for node in ast.walk(callout_function):
            if not isinstance(node, ast.Return) or not isinstance(node.value, ast.Dict):
                continue
            fields = {
                key.value: value
                for key, value in zip(node.value.keys, node.value.values)
                if isinstance(key, ast.Constant) and isinstance(key.value, str)
            }
            success = fields.get("success")
            if not (
                isinstance(success, ast.Constant)
                and success.value is False
                and "message" in fields
            ):
                continue
            precondition_returns.append(fields)

        self.assertEqual(len(precondition_returns), 5)
        for fields in precondition_returns:
            self.assertIn("info", fields)
            self.assertEqual(
                ast.dump(fields["info"]),
                ast.dump(fields["message"]),
            )

    def test_uses_font_metrics_dpi_for_wrapping_and_height(self):
        metrics = _FakeFontMetrics(dpi=72.0)
        text = (
            "1934年11月中旬，合编后的主力部队在平顶山发生战斗。"
            "战斗结束后部队分两路：主力南进南阳，支队西南向内乡。"
        )

        lines, line_height_mm, total_height_mm = wrap_chinese_text_to_mm(
            text,
            metrics,
            61.4,
        )

        self.assertGreater(len(lines), 1)
        self.assertAlmostEqual(font_metrics_mm_per_pixel(metrics), 25.4 / 72.0)
        self.assertAlmostEqual(line_height_mm, 6.0 * 25.4 / 72.0)
        self.assertAlmostEqual(total_height_mm, line_height_mm * len(lines))
        for line in lines:
            line_width_mm = (
                metrics.horizontalAdvance(line)
                * font_metrics_mm_per_pixel(metrics)
            )
            self.assertLessEqual(line_width_mm, 61.4 - 0.8)

    def test_punctuation_rule_never_pushes_line_past_box_width(self):
        metrics = _FakeFontMetrics(dpi=72.0, char_width_px=5.0)
        lines, _, _ = wrap_chinese_text_to_mm(
            "一二三四五六七八九，后续文字",
            metrics,
            16.0,
            safety_margin_mm=0.0,
        )

        max_width_px = 16.0 / font_metrics_mm_per_pixel(metrics)
        for line in lines:
            self.assertLessEqual(metrics.horizontalAdvance(line), max_width_px)

    def test_invalid_font_dpi_uses_safe_fallback(self):
        metrics = _FakeFontMetrics(dpi=0.0)
        self.assertAlmostEqual(
            font_metrics_mm_per_pixel(metrics),
            25.4 / 96.0,
        )


if __name__ == "__main__":
    unittest.main()
