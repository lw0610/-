from pathlib import Path
from PIL import Image, ImageDraw, ImageFont


ROOT = Path("/Users/zw/研究生/QGIS-agent/Eino")
OUT = ROOT / "docs" / "paper_figures" / "prototypes"
W, H = 2200, 1400


COL = {
    "ink": "#172033",
    "muted": "#667085",
    "line": "#AEBBCC",
    "line2": "#CFD8E3",
    "blue": "#E8F2F9",
    "blue2": "#B7D0E6",
    "green": "#EAF6F0",
    "green2": "#9BC7B5",
    "yellow": "#FFF4CF",
    "purple": "#F0ECFA",
    "rose": "#FCECEF",
    "gray": "#F7F9FC",
    "teal": "#147C78",
}


def font(size: int):
    candidates = [
        "/System/Library/Fonts/Hiragino Sans GB.ttc",
        "/System/Library/Fonts/STHeiti Medium.ttc",
        "/System/Library/Fonts/Supplemental/Songti.ttc",
    ]
    for p in candidates:
        if Path(p).exists():
            return ImageFont.truetype(p, size=size)
    return ImageFont.load_default()


def text_size(d, text, f):
    b = d.textbbox((0, 0), text, font=f)
    return b[2] - b[0], b[3] - b[1]


def center(d, x1, y, x2, text, size=24, fill=None):
    f = font(size)
    tw, _ = text_size(d, text, f)
    d.text((x1 + (x2 - x1 - tw) / 2, y), text, font=f, fill=fill or COL["ink"])


def wrap(d, text, size, width):
    f = font(size)
    lines, cur = [], ""
    for ch in text:
        trial = cur + ch
        if text_size(d, trial, f)[0] <= width:
            cur = trial
        else:
            if cur:
                lines.append(cur)
            cur = ch
    if cur:
        lines.append(cur)
    return lines


def arrow(d, p1, p2, color=None, width=3):
    import math
    color = color or COL["muted"]
    d.line([p1, p2], fill=color, width=width)
    ang = math.atan2(p2[1] - p1[1], p2[0] - p1[0])
    s = 13
    a = (p2[0] - s * math.cos(ang - math.pi / 6), p2[1] - s * math.sin(ang - math.pi / 6))
    b = (p2[0] - s * math.cos(ang + math.pi / 6), p2[1] - s * math.sin(ang + math.pi / 6))
    d.polygon([p2, a, b], fill=color)


def box(d, xy, title, body="", fill=None, outline=None, title_size=25, body_size=18):
    x1, y1, x2, y2 = xy
    d.rounded_rectangle(xy, radius=16, fill=fill or "white", outline=outline or COL["line2"], width=2)
    center(d, x1 + 12, y1 + 22, x2 - 12, title, title_size)
    yy = y1 + 62
    for line in wrap(d, body, body_size, x2 - x1 - 36):
        center(d, x1 + 18, yy, x2 - 18, line, body_size, COL["muted"])
        yy += body_size + 10


def title(d, text, sub):
    d.text((80, 52), text, font=font(42), fill=COL["ink"])
    d.text((80, 108), sub, font=font(23), fill=COL["muted"])


def panel_label(d, x, y, lab, text):
    d.text((x, y), lab, font=font(32), fill=COL["ink"])
    d.text((x + 50, y + 4), text, font=font(27), fill=COL["ink"])


def route_map(d, xy):
    x1, y1, x2, y2 = xy
    d.rounded_rectangle(xy, radius=18, fill="#FFFFFF", outline=COL["line2"], width=2)
    # Province-like outline.
    pts = [
        (x1 + 110, y1 + 225), (x1 + 220, y1 + 120), (x1 + 390, y1 + 92),
        (x1 + 520, y1 + 150), (x1 + 570, y1 + 280), (x1 + 500, y1 + 410),
        (x1 + 330, y1 + 455), (x1 + 180, y1 + 380)
    ]
    d.line(pts + [pts[0]], fill="#CBD5E1", width=4)
    path = [(x1 + 155, y1 + 350), (x1 + 260, y1 + 300), (x1 + 365, y1 + 245), (x1 + 470, y1 + 165)]
    d.line(path, fill=COL["teal"], width=8, joint="curve")
    for i, (x, y) in enumerate(path):
        d.ellipse((x - 15, y - 15, x + 15, y + 15), fill="#FFFFFF", outline=COL["teal"], width=5)
        d.text((x + 18, y - 22), f"N{i+1}", font=font(18), fill=COL["ink"])
    arrow(d, (path[-2][0] + 35, path[-2][1] - 18), path[-1], COL["teal"], 4)
    center(d, x1, y2 - 42, x2, "路线类专题图成果示意", 19, COL["muted"])


def draw_overview():
    img = Image.new("RGB", (W, H), "white")
    d = ImageDraw.Draw(img)
    title(d, "Fig. A  方法总览：从路线叙事到可验证地图成果", "强调表示转换和证据链，而不是软件执行流程")
    panel_label(d, 90, 190, "a", "问题输入")
    box(d, (100, 250, 620, 530), "路线叙事文本", "某部队自 A 地出发，经 B、C 至 D，并在 C 地发生关键事件。", COL["gray"])
    box(d, (100, 575, 620, 810), "空间与制图资源", "行政边界、底图、节点坐标、QGIS 工程状态", COL["blue"])
    panel_label(d, 760, 190, "b", "中间表示")
    box(d, (735, 260, 1110, 460), "语义状态", "主体、时间、地点、节点顺序、事件说明", COL["green"])
    box(d, (735, 520, 1110, 720), "知识状态", "本体概念、图谱关系、表达约束、质量规则", COL["yellow"])
    box(d, (735, 780, 1110, 980), "执行状态", "工具调用、图层变化、布局对象、导出结果", COL["purple"])
    panel_label(d, 1275, 190, "c", "知识约束执行")
    box(d, (1245, 280, 1545, 430), "多智能体决策", "解析、推理、表达、编排、评价", "white")
    box(d, (1645, 280, 1945, 430), "QGIS 工具契约", "输入、输出、条件、失败反馈", "white")
    d.line([(1395, 500), (1395, 690), (1795, 690), (1795, 500)], fill=COL["line"], width=3)
    center(d, 1245, 715, 1945, "结构化状态在智能体与工具之间循环更新", 22, COL["muted"])
    route_map(d, (1265, 790, 1985, 1280))
    arrow(d, (620, 395), (735, 360))
    arrow(d, (620, 690), (735, 620))
    arrow(d, (1110, 360), (1245, 355))
    arrow(d, (1110, 620), (1245, 355))
    arrow(d, (1545, 355), (1645, 355))
    arrow(d, (1795, 430), (1650, 790))
    img.save(OUT / "figA_method_overview.png", dpi=(300, 300))


def draw_knowledge():
    img = Image.new("RGB", (W, H), "white")
    d = ImageDraw.Draw(img)
    title(d, "Fig. B  路线制图语义表示与知识约束", "用知识图谱表达路线节点、事件、地图要素和质量约束的关系")
    panel_label(d, 90, 190, "a", "文本到结构化语义")
    box(d, (100, 255, 770, 465), "原始路线材料", "从 A 地出发，经 B 地转移，于 C 地发生战斗，随后抵达 D 地。", COL["gray"])
    box(d, (100, 560, 770, 930), "结构化抽取结果", "RouteNode: [A, B, C, D]\nEvent: battle(C)\nOrder: A→B→C→D\nMapNeed: route line, node label, event callout", "#FFFFFF")
    arrow(d, (435, 465), (435, 560))

    panel_label(d, 930, 190, "b", "本体关系")
    nodes = {
        "制图任务": (1410, 610, 115, 52, COL["blue"]),
        "路线节点": (1160, 430, 100, 42, COL["green"]),
        "历史事件": (1150, 790, 100, 42, COL["yellow"]),
        "路线段": (1665, 430, 90, 42, COL["green"]),
        "表达方法": (1690, 790, 100, 42, COL["purple"]),
        "地图要素": (1410, 285, 100, 42, COL["purple"]),
        "质量约束": (1890, 610, 100, 42, COL["rose"]),
        "输出成果": (960, 610, 100, 42, "#E5E7EB"),
    }
    edges = [
        ("制图任务", "路线节点", "包含"),
        ("路线节点", "路线段", "连接"),
        ("路线节点", "历史事件", "承载"),
        ("路线段", "表达方法", "采用"),
        ("表达方法", "地图要素", "生成"),
        ("制图任务", "质量约束", "受控于"),
        ("制图任务", "输出成果", "产生"),
        ("地图要素", "制图任务", "服务于"),
    ]
    for a, b, lab in edges:
        ax, ay, *_ = nodes[a]
        bx, by, *_ = nodes[b]
        d.line([(ax, ay), (bx, by)], fill=COL["line"], width=3)
        mx, my = (ax + bx) / 2, (ay + by) / 2
        box(d, (mx - 48, my - 22, mx + 48, my + 22), lab, "", "#FFFFFF", COL["line2"], 16, 10)
    for name, (cx, cy, rx, ry, fill) in nodes.items():
        d.ellipse((cx - rx, cy - ry, cx + rx, cy + ry), fill=fill, outline="#8291A3", width=3)
        center(d, cx - rx, cy - 14, cx + rx, name, 21)

    panel_label(d, 90, 1035, "c", "约束如何进入工具执行")
    chips = [
        ("字段完整性", "节点顺序、事件地点、表达要求不能为空"),
        ("关系一致性", "事件必须挂接到路线节点或空间位置"),
        ("表达规则", "主路线、分支路线、重要事件采用不同层级"),
        ("评价规则", "节点完整性、路线连贯性、图例一致性"),
    ]
    x = 180
    for name, desc in chips:
        box(d, (x, 1110, x + 440, 1270), name, desc, "#FFFFFF")
        x += 485
    img.save(OUT / "figB_knowledge_representation.png", dpi=(300, 300))


def draw_agent_tool():
    img = Image.new("RGB", (W, H), "white")
    d = ImageDraw.Draw(img)
    title(d, "Fig. C  多智能体协同与工具契约", "重点展示角色分工、状态传递和工具契约，不展示软件流水线")
    panel_label(d, 90, 190, "a", "角色分工")
    roles = [
        ("协调", "维护目标与任务状态"),
        ("意图解析", "识别主体、范围、成果形式"),
        ("数据推理", "检查图层、字段、坐标"),
        ("路线表达", "生成路线、节点、事件表达"),
        ("版式编排", "组织标题、图例、说明框"),
        ("质量评价", "定位问题并生成修订建议"),
    ]
    for i, (r, b) in enumerate(roles):
        x = 110 + (i % 3) * 345
        y = 270 + (i // 3) * 210
        box(d, (x, y, x + 285, y + 150), r, b, "#FFFFFF")
    panel_label(d, 1220, 190, "b", "结构化状态传递")
    states = [
        ("制图需求", COL["blue"]),
        ("图谱关系", COL["yellow"]),
        ("工具计划", COL["purple"]),
        ("执行结果", COL["green"]),
        ("评价反馈", COL["rose"]),
    ]
    for i, (s, c) in enumerate(states):
        box(d, (1280, 275 + i * 145, 1765, 365 + i * 145), s, "", c)
        if i < len(states) - 1:
            arrow(d, (1520, 365 + i * 145), (1520, 420 + i * 145), COL["muted"], 3)
    d.arc((1170, 275, 2050, 965), 278, 82, fill=COL["teal"], width=5)
    arrow(d, (1900, 480), (1790, 330), COL["teal"], 4)

    panel_label(d, 90, 800, "c", "工具契约")
    headers = ["工具", "输入", "输出", "失败反馈"]
    rows = [
        ["data_load", "路径/图层名", "QGIS 图层", "路径无效/字段缺失"],
        ["route_build", "节点顺序", "路线段图层", "节点断裂/坐标缺失"],
        ["layout_export", "布局参数", "PDF/PNG", "元素缺失/导出失败"],
    ]
    x0, y0 = 130, 895
    ws = [260, 390, 350, 470]
    h = 70
    for j, head in enumerate(headers):
        x = x0 + sum(ws[:j])
        d.rectangle((x, y0, x + ws[j], y0 + h), fill="#EEF3F7", outline=COL["line2"], width=2)
        center(d, x, y0 + 22, x + ws[j], head, 20)
    for i, row in enumerate(rows):
        yy = y0 + (i + 1) * h
        for j, val in enumerate(row):
            x = x0 + sum(ws[:j])
            d.rectangle((x, yy, x + ws[j], yy + h), fill="#FFFFFF", outline=COL["line2"], width=2)
            center(d, x, yy + 22, x + ws[j], val, 19, COL["muted"] if j else COL["ink"])
    box(d, (1710, 960, 2070, 1185), "关键机制", "智能体不直接写长脚本，而是生成满足工具契约的可检查调用。", COL["gray"])
    img.save(OUT / "figC_agent_tool_contract.png", dpi=(300, 300))


def draw_quality():
    img = Image.new("RGB", (W, H), "white")
    d = ImageDraw.Draw(img)
    title(d, "Fig. D  地图质量评价与反馈优化", "将知识图谱、QGIS 状态和地图图像共同作为评价证据")
    panel_label(d, 90, 190, "a", "评价闭环")
    box(d, (180, 315, 520, 450), "候选地图", "QGIS 导出图像与布局", COL["blue"])
    box(d, (690, 315, 1030, 450), "规则检查", "节点、图例、地图要素", COL["yellow"])
    box(d, (690, 650, 1030, 785), "视觉评价", "遮挡、层次、可读性", COL["purple"])
    box(d, (180, 650, 520, 785), "局部修订", "标注、线型、说明框", COL["green"])
    arrow(d, (520, 382), (690, 382))
    arrow(d, (860, 450), (860, 650))
    arrow(d, (690, 720), (520, 720))
    arrow(d, (350, 650), (350, 450))
    d.ellipse((495, 495, 715, 595), fill="#FFFFFF", outline=COL["line"], width=3)
    center(d, 495, 532, 715, "评价状态", 23)

    panel_label(d, 1220, 190, "b", "质量证据矩阵")
    dims = ["路线连贯性", "节点完整性", "事件匹配", "符号层次", "版式可读性"]
    sources = ["知识图谱", "QGIS状态", "地图图像"]
    marks = [
        ["●", "●", "○"],
        ["●", "●", "○"],
        ["●", "○", "●"],
        ["○", "●", "●"],
        ["○", "●", "●"],
    ]
    x0, y0 = 1225, 310
    cw, rh = 205, 78
    for j, head in enumerate(["评价维度"] + sources):
        d.rectangle((x0 + j * cw, y0, x0 + (j + 1) * cw, y0 + rh), fill="#EEF3F7", outline=COL["line2"], width=2)
        center(d, x0 + j * cw, y0 + 25, x0 + (j + 1) * cw, head, 20)
    for i, dim in enumerate(dims):
        yy = y0 + (i + 1) * rh
        d.rectangle((x0, yy, x0 + cw, yy + rh), fill="#FFFFFF", outline=COL["line2"], width=2)
        center(d, x0, yy + 25, x0 + cw, dim, 19)
        for j, m in enumerate(marks[i]):
            xx = x0 + (j + 1) * cw
            d.rectangle((xx, yy, xx + cw, yy + rh), fill="#FFFFFF", outline=COL["line2"], width=2)
            center(d, xx, yy + 18, xx + cw, m, 30, COL["teal"] if m == "●" else COL["line"])
    d.text((1225, 780), "● 主要证据源    ○ 辅助证据源", font=font(20), fill=COL["muted"])

    panel_label(d, 90, 930, "c", "评价结果转化为修订动作")
    fixes = [
        ("关键节点缺失", "调用节点标注工具"),
        ("路线层次不清", "调整线宽、线型、颜色"),
        ("说明框遮挡路线", "重新计算说明框位置"),
        ("图例项不完整", "补充符号与说明"),
    ]
    for i, (issue, fix) in enumerate(fixes):
        x = 130 + i * 500
        box(d, (x, 1020, x + 420, 1185), issue, fix, "#FFFFFF")
        if i < len(fixes) - 1:
            arrow(d, (x + 420, 1100), (x + 485, 1100), COL["line"], 2)
    img.save(OUT / "figD_quality_feedback.png", dpi=(300, 300))


def main():
    OUT.mkdir(parents=True, exist_ok=True)
    draw_overview()
    draw_knowledge()
    draw_agent_tool()
    draw_quality()
    for p in sorted(OUT.glob("*.png")):
        print(p)


if __name__ == "__main__":
    main()
