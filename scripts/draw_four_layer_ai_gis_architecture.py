from pathlib import Path

from PIL import Image, ImageDraw, ImageFont


ROOT = Path("/Users/zw/研究生/QGIS-agent/Eino")
OUT_SVG = ROOT / "docs" / "paper_figures" / "four_layer_ai_gis_architecture.svg"
OUT_PNG = ROOT / "docs" / "paper_figures" / "four_layer_ai_gis_architecture.png"


W, H = 2400, 1500


def font(size: int):
    candidates = [
        "/System/Library/Fonts/Hiragino Sans GB.ttc",
        "/System/Library/Fonts/STHeiti Medium.ttc",
        "/System/Library/Fonts/Supplemental/Songti.ttc",
    ]
    for path in candidates:
        if Path(path).exists():
            return ImageFont.truetype(path, size=size)
    return ImageFont.load_default()


def text_size(draw, text, f):
    box = draw.textbbox((0, 0), text, font=f)
    return box[2] - box[0], box[3] - box[1]


def draw_center(draw, x1, y, x2, text, f, fill="#1E293B"):
    tw, _ = text_size(draw, text, f)
    draw.text((x1 + (x2 - x1 - tw) / 2, y), text, font=f, fill=fill)


def draw_arrow(draw, start, end, fill="#64748B", width=4):
    import math

    draw.line([start, end], fill=fill, width=width)
    ang = math.atan2(end[1] - start[1], end[0] - start[0])
    size = 14
    p1 = (end[0] - size * math.cos(ang - math.pi / 6), end[1] - size * math.sin(ang - math.pi / 6))
    p2 = (end[0] - size * math.cos(ang + math.pi / 6), end[1] - size * math.sin(ang + math.pi / 6))
    draw.polygon([end, p1, p2], fill=fill)


def pil_box(draw, xy, title, body, fill="#F8FAFC", outline="#CBD5E1"):
    x1, y1, x2, y2 = xy
    draw.rounded_rectangle(xy, radius=14, fill=fill, outline=outline, width=2)
    draw_center(draw, x1, y1 + 20, x2, title, font(25), "#0F172A")
    yy = y1 + 58
    for line in wrap_cn(body, 16):
        draw_center(draw, x1 + 14, yy, x2 - 14, line, font(19), "#475569")
        yy += 29


def esc(s: str) -> str:
    return (
        s.replace("&", "&amp;")
        .replace("<", "&lt;")
        .replace(">", "&gt;")
        .replace('"', "&quot;")
    )


def wrap_cn(text: str, width: int) -> list[str]:
    lines, cur = [], ""
    for ch in text:
        if len(cur) >= width and ch not in "，、； ":
            lines.append(cur)
            cur = ch
        else:
            cur += ch
    if cur:
        lines.append(cur)
    return lines


class SVG:
    def __init__(self):
        self.parts = []

    def add(self, s: str):
        self.parts.append(s)

    def text(self, x, y, text, size=28, weight=400, fill="#1E293B", anchor="middle"):
        self.add(
            f'<text x="{x}" y="{y}" text-anchor="{anchor}" '
            f'font-family="Arial, Helvetica, sans-serif" font-size="{size}" '
            f'font-weight="{weight}" fill="{fill}">{esc(text)}</text>'
        )

    def line(self, x1, y1, x2, y2, color="#64748B", width=3, marker=True):
        marker_attr = ' marker-end="url(#arrow)"' if marker else ""
        self.add(
            f'<line x1="{x1}" y1="{y1}" x2="{x2}" y2="{y2}" '
            f'stroke="{color}" stroke-width="{width}" stroke-linecap="round"{marker_attr}/>'
        )

    def rect(self, x, y, w, h, fill="#F8FAFC", stroke="#CBD5E1", sw=2, rx=16):
        self.add(
            f'<rect x="{x}" y="{y}" width="{w}" height="{h}" rx="{rx}" '
            f'fill="{fill}" stroke="{stroke}" stroke-width="{sw}"/>'
        )

    def box(self, x, y, w, h, title, body, fill="#F8FAFC", stroke="#CBD5E1"):
        self.rect(x, y, w, h, fill, stroke, 2, 14)
        self.text(x + w / 2, y + 42, title, 25, 600, "#0F172A")
        for i, line in enumerate(wrap_cn(body, 16)):
            self.text(x + w / 2, y + 78 + i * 29, line, 19, 400, "#475569")


def build_svg() -> str:
    s = SVG()
    s.add(
        f'''<svg xmlns="http://www.w3.org/2000/svg" width="{W}" height="{H}" viewBox="0 0 {W} {H}">
<defs>
  <marker id="arrow" markerWidth="12" markerHeight="12" refX="9" refY="3.5" orient="auto">
    <path d="M0,0 L10,3.5 L0,7 Z" fill="#64748B"/>
  </marker>
</defs>
<rect width="{W}" height="{H}" fill="#FFFFFF"/>
'''
    )

    s.text(120, 90, "四层 GIS 与 AI 智能体融合系统技术架构", 42, 700, "#0F172A", "start")
    s.text(
        120,
        132,
        "从用户交互、Go 后端调度、多智能体协同到 QGIS 本地执行的分层集成关系",
        24,
        400,
        "#64748B",
        "start",
    )

    layer_x, layer_w = 130, 2140
    layer_h, gap = 245, 55
    top_y = 205
    layers = [
        ("用户交互层", "User Interaction", "#EAF4FB", "#8EB6D8"),
        ("Go 后端调度层", "Go Backend Orchestration", "#EEF3F7", "#94A3B8"),
        ("多智能体层", "Multi-agent Reasoning", "#F4F7FA", "#A7B4C2"),
        ("QGIS 执行层", "QGIS Execution", "#E8F3F1", "#8FBDB4"),
    ]

    modules = [
        [
            ("Tauri 桌面端", "对话入口、项目状态、模型配置"),
            ("任务面板", "制图需求、运行进度、结果反馈"),
            ("地图交互", "图层浏览、布局预览、导出查看"),
            ("配置管理", "模型、路径、技能与工具设置"),
        ],
        [
            ("API 路由", "接收前端请求并统一鉴权与参数校验"),
            ("运行时管理", "模型会话、任务上下文与状态维护"),
            ("任务调度器", "拆分任务、选择智能体、组织工具序列"),
            ("工具网关", "封装 QGIS 工具、日志与异常反馈"),
        ],
        [
            ("协调智能体", "维护全局目标、计划与执行状态"),
            ("意图解析智能体", "识别路线图任务、范围与成果形式"),
            ("数据推理智能体", "检查图层、字段、坐标与数据可用性"),
            ("制图智能体", "生成路线表达、符号配置与布局策略"),
            ("评价智能体", "检查完整性、一致性与视觉可读性"),
        ],
        [
            ("PyQGIS 桥接", "连接后端工具调用与 QGIS 工程对象"),
            ("数据处理", "加载矢量/栅格、坐标转换、字段读取"),
            ("制图渲染", "路线生成、符号化、标注与图层控制"),
            ("布局导出", "标题、图例、比例尺、成果导出"),
        ],
    ]

    centers = []
    for idx, (cn, en, fill, stroke) in enumerate(layers):
        y = top_y + idx * (layer_h + gap)
        centers.append(y + layer_h / 2)
        s.rect(layer_x, y, layer_w, layer_h, "#FFFFFF", stroke, 2, 22)
        s.rect(layer_x, y, 250, layer_h, fill, stroke, 2, 22)
        s.text(layer_x + 125, y + 102, cn, 31, 700, "#0F172A")
        s.text(layer_x + 125, y + 140, en, 18, 400, "#64748B")

        count = len(modules[idx])
        start_x = layer_x + 305
        available = layer_w - 360
        box_gap = 32
        box_w = (available - box_gap * (count - 1)) / count
        box_h = 140
        box_y = y + 55
        box_centers = []
        for m_i, (title, body) in enumerate(modules[idx]):
            x = start_x + m_i * (box_w + box_gap)
            box_centers.append((x + box_w / 2, box_y + box_h / 2))
            s.box(x, box_y, box_w, box_h, title, body, "#F8FAFC", "#CBD5E1")

        for a, b in zip(box_centers, box_centers[1:]):
            s.line(a[0] + box_w / 2 - 16, a[1], b[0] - box_w / 2 + 16, b[1], "#94A3B8", 2)

    # Vertical cross-layer dependencies.
    for i in range(3):
        x = layer_x + layer_w / 2
        y1 = top_y + i * (layer_h + gap) + layer_h
        y2 = top_y + (i + 1) * (layer_h + gap)
        s.line(x, y1 + 8, x, y2 - 8, "#64748B", 4)

    labels = [
        ("自然语言任务", centers[0] + 155),
        ("结构化调度指令", centers[1] + 155),
        ("工具调用与状态反馈", centers[2] + 155),
    ]
    for text, y in labels:
        s.rect(980, y - 24, 440, 48, "#FFFFFF", "#CBD5E1", 1, 22)
        s.text(1200, y + 8, text, 20, 500, "#475569")

    # Side feedback channel.
    feedback_x = 2295
    s.line(feedback_x, top_y + 3 * (layer_h + gap) + 65, feedback_x, top_y + 145, "#64748B", 3)
    s.line(feedback_x, top_y + 145, layer_x + layer_w + 10, top_y + 145, "#64748B", 3)
    s.text(feedback_x - 35, 805, "运行日志 / 工具结果 / 成果评价反馈", 22, 500, "#475569", "end")

    # Bottom note.
    s.rect(130, 1390, 2140, 58, "#F8FAFC", "#CBD5E1", 1, 18)
    s.text(
        1200,
        1427,
        "技术要点：Go 负责运行时调度与工具网关，多智能体负责语义推理和制图决策，QGIS/PyQGIS 负责本地数据处理、地图渲染与成果导出。",
        21,
        400,
        "#334155",
    )

    s.add("</svg>\n")
    return "\n".join(s.parts)


def make_png():
    img = Image.new("RGB", (W, H), "white")
    d = ImageDraw.Draw(img)

    d.text((120, 58), "四层 GIS 与 AI 智能体融合系统技术架构", font=font(42), fill="#0F172A")
    d.text(
        (120, 112),
        "从用户交互、Go 后端调度、多智能体协同到 QGIS 本地执行的分层集成关系",
        font=font(24),
        fill="#64748B",
    )

    layer_x, layer_w = 130, 2140
    layer_h, gap = 245, 55
    top_y = 205
    layers = [
        ("用户交互层", "User Interaction", "#EAF4FB", "#8EB6D8"),
        ("Go 后端调度层", "Go Backend Orchestration", "#EEF3F7", "#94A3B8"),
        ("多智能体层", "Multi-agent Reasoning", "#F4F7FA", "#A7B4C2"),
        ("QGIS 执行层", "QGIS Execution", "#E8F3F1", "#8FBDB4"),
    ]
    modules = [
        [
            ("Tauri 桌面端", "对话入口、项目状态、模型配置"),
            ("任务面板", "制图需求、运行进度、结果反馈"),
            ("地图交互", "图层浏览、布局预览、导出查看"),
            ("配置管理", "模型、路径、技能与工具设置"),
        ],
        [
            ("API 路由", "接收前端请求并统一鉴权与参数校验"),
            ("运行时管理", "模型会话、任务上下文与状态维护"),
            ("任务调度器", "拆分任务、选择智能体、组织工具序列"),
            ("工具网关", "封装 QGIS 工具、日志与异常反馈"),
        ],
        [
            ("协调智能体", "维护全局目标、计划与执行状态"),
            ("意图解析智能体", "识别路线图任务、范围与成果形式"),
            ("数据推理智能体", "检查图层、字段、坐标与数据可用性"),
            ("制图智能体", "生成路线表达、符号配置与布局策略"),
            ("评价智能体", "检查完整性、一致性与视觉可读性"),
        ],
        [
            ("PyQGIS 桥接", "连接后端工具调用与 QGIS 工程对象"),
            ("数据处理", "加载矢量/栅格、坐标转换、字段读取"),
            ("制图渲染", "路线生成、符号化、标注与图层控制"),
            ("布局导出", "标题、图例、比例尺、成果导出"),
        ],
    ]

    centers = []
    for idx, (cn, en, fill, stroke) in enumerate(layers):
        y = top_y + idx * (layer_h + gap)
        centers.append(y + layer_h / 2)
        d.rounded_rectangle((layer_x, y, layer_x + layer_w, y + layer_h), radius=22, fill="#FFFFFF", outline=stroke, width=2)
        d.rounded_rectangle((layer_x, y, layer_x + 250, y + layer_h), radius=22, fill=fill, outline=stroke, width=2)
        draw_center(d, layer_x, y + 84, layer_x + 250, cn, font(31), "#0F172A")
        draw_center(d, layer_x, y + 126, layer_x + 250, en, font(18), "#64748B")

        count = len(modules[idx])
        start_x = layer_x + 305
        available = layer_w - 360
        box_gap = 32
        box_w = (available - box_gap * (count - 1)) / count
        box_h = 140
        box_y = y + 55
        box_centers = []
        for m_i, (title, body) in enumerate(modules[idx]):
            x = start_x + m_i * (box_w + box_gap)
            box_centers.append((x + box_w / 2, box_y + box_h / 2))
            pil_box(d, (x, box_y, x + box_w, box_y + box_h), title, body, "#F8FAFC", "#CBD5E1")

        for a, b in zip(box_centers, box_centers[1:]):
            draw_arrow(d, (a[0] + box_w / 2 - 18, a[1]), (b[0] - box_w / 2 + 18, b[1]), "#94A3B8", 2)

    for i in range(3):
        x = layer_x + layer_w / 2
        y1 = top_y + i * (layer_h + gap) + layer_h
        y2 = top_y + (i + 1) * (layer_h + gap)
        draw_arrow(d, (x, y1 + 8), (x, y2 - 8), "#64748B", 4)

    labels = [
        ("自然语言任务", centers[0] + 155),
        ("结构化调度指令", centers[1] + 155),
        ("工具调用与状态反馈", centers[2] + 155),
    ]
    for text, y in labels:
        d.rounded_rectangle((980, y - 24, 1420, y + 24), radius=22, fill="#FFFFFF", outline="#CBD5E1", width=1)
        draw_center(d, 980, y - 14, 1420, text, font(20), "#475569")

    feedback_x = 2295
    draw_arrow(d, (feedback_x, top_y + 3 * (layer_h + gap) + 65), (feedback_x, top_y + 145), "#64748B", 3)
    draw_arrow(d, (feedback_x, top_y + 145), (layer_x + layer_w + 10, top_y + 145), "#64748B", 3)
    tw, th = text_size(d, "运行日志 / 工具结果 / 成果评价反馈", font(22))
    d.text((feedback_x - 55 - tw, 785), "运行日志 / 工具结果 / 成果评价反馈", font=font(22), fill="#475569")

    d.rounded_rectangle((130, 1390, 2270, 1448), radius=18, fill="#F8FAFC", outline="#CBD5E1", width=1)
    draw_center(
        d,
        130,
        1407,
        2270,
        "技术要点：Go 负责运行时调度与工具网关，多智能体负责语义推理和制图决策，QGIS/PyQGIS 负责本地数据处理、地图渲染与成果导出。",
        font(21),
        "#334155",
    )
    img.save(OUT_PNG, dpi=(300, 300))


def main():
    OUT_SVG.parent.mkdir(parents=True, exist_ok=True)
    OUT_SVG.write_text(build_svg(), encoding="utf-8")
    try:
        import cairosvg

        cairosvg.svg2png(url=str(OUT_SVG), write_to=str(OUT_PNG), output_width=W, output_height=H)
    except Exception as exc:
        print(f"PNG export via cairosvg skipped: {exc}")
        make_png()
    print(OUT_SVG)
    print(OUT_PNG)


if __name__ == "__main__":
    main()
