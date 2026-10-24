from pathlib import Path
import math
import textwrap

from PIL import Image, ImageDraw, ImageFont
from docx import Document
from docx.enum.table import WD_CELL_VERTICAL_ALIGNMENT, WD_TABLE_ALIGNMENT
from docx.enum.text import WD_ALIGN_PARAGRAPH
from docx.oxml import OxmlElement
from docx.oxml.ns import qn
from docx.shared import Cm, Pt, RGBColor


ROOT = Path("/Users/zw/研究生/QGIS-agent/Eino")
OUT = ROOT / "AutoCarto_专题图制图智能体论文_重写修订稿.docx"
FIG_DIR = ROOT / "docs" / "autocarto_paper_figures"


FONT_CANDIDATES = [
    "/System/Library/Fonts/PingFang.ttc",
    "/System/Library/Fonts/Hiragino Sans GB.ttc",
    "/System/Library/Fonts/STHeiti Light.ttc",
    "/Library/Fonts/Arial Unicode.ttf",
]


def get_font(size, bold=False):
    for path in FONT_CANDIDATES:
        if Path(path).exists():
            return ImageFont.truetype(path, size=size)
    return ImageFont.load_default()


def wrap_text(draw, text, font, width):
    lines = []
    current = ""
    for ch in text:
        test = current + ch
        if draw.textbbox((0, 0), test, font=font)[2] <= width:
            current = test
        else:
            if current:
                lines.append(current)
            current = ch
    if current:
        lines.append(current)
    return lines


def draw_round_box(draw, box, text, fill, outline="#4B5563", font_size=24, title=False):
    x1, y1, x2, y2 = box
    draw.rounded_rectangle(box, radius=16, fill=fill, outline=outline, width=2)
    font = get_font(font_size)
    max_w = x2 - x1 - 28
    lines = wrap_text(draw, text, font, max_w)
    line_h = font_size + 8
    total_h = len(lines) * line_h
    y = y1 + (y2 - y1 - total_h) / 2
    for line in lines:
        tw = draw.textbbox((0, 0), line, font=font)[2]
        draw.text((x1 + (x2 - x1 - tw) / 2, y), line, fill="#111827", font=font)
        y += line_h


def arrow(draw, start, end, color="#374151", width=4):
    draw.line([start, end], fill=color, width=width)
    angle = math.atan2(end[1] - start[1], end[0] - start[0])
    size = 14
    p1 = (end[0] - size * math.cos(angle - math.pi / 6), end[1] - size * math.sin(angle - math.pi / 6))
    p2 = (end[0] - size * math.cos(angle + math.pi / 6), end[1] - size * math.sin(angle + math.pi / 6))
    draw.polygon([end, p1, p2], fill=color)


def save_canvas(path, w=1800, h=980):
    img = Image.new("RGB", (w, h), "#FFFFFF")
    return img, ImageDraw.Draw(img)


def make_figures():
    FIG_DIR.mkdir(parents=True, exist_ok=True)
    title_font = get_font(34)
    small_font = get_font(22)

    # Figure 1: Overall framework
    img, draw = save_canvas(FIG_DIR / "framework.png", 1800, 980)
    draw.text((60, 38), "路线类专题图自动制图总体框架", fill="#111827", font=title_font)
    boxes = [
        ((80, 190, 330, 330), "自然语言需求\n路线叙事文本", "#E0F2FE"),
        ((410, 190, 660, 330), "本体约束\n信息抽取", "#ECFDF5"),
        ((740, 190, 990, 330), "路线制图\n知识图谱", "#FEEFCA"),
        ((1070, 190, 1320, 330), "多智能体\n任务规划", "#FCE7F3"),
        ((1400, 190, 1650, 330), "QGIS工具链\n制图执行", "#EDE9FE"),
    ]
    for box, text, fill in boxes:
        draw_round_box(draw, box, text, fill)
    for sx, ex in [((330, 260), (410, 260)), ((660, 260), (740, 260)), ((990, 260), (1070, 260)), ((1320, 260), (1400, 260))]:
        arrow(draw, sx, ex)
    draw_round_box(draw, (410, 560, 660, 700), "质量评价\n与反馈修订", "#FEE2E2")
    draw_round_box(draw, (740, 560, 990, 700), "地图成果\nPDF/PNG/布局", "#E5E7EB")
    arrow(draw, (1525, 330), (890, 560), "#B91C1C")
    arrow(draw, (740, 630), (660, 630), "#B91C1C")
    arrow(draw, (535, 560), (535, 330), "#B91C1C")
    draw.text((80, 820), "框架重点：先将路线叙事转化为结构化制图知识，再由智能体调用本地GIS工具执行，最后依据质量评价结果回写修订。", fill="#374151", font=small_font)
    img.save(FIG_DIR / "framework.png")

    # Figure 2: Ontology and KG
    img, draw = save_canvas(FIG_DIR / "ontology.png", 1800, 1050)
    draw.text((60, 38), "路线类专题图本体与知识图谱核心关系", fill="#111827", font=title_font)
    nodes = {
        "制图任务": (770, 120, 1030, 230, "#DBEAFE"),
        "行动主体": (160, 330, 420, 440, "#E0F2FE"),
        "时间信息": (500, 330, 760, 440, "#ECFDF5"),
        "路线节点": (840, 330, 1100, 440, "#FEEFCA"),
        "历史事件": (1180, 330, 1440, 440, "#FCE7F3"),
        "路线关系": (500, 590, 760, 700, "#F3E8FF"),
        "表达方法": (840, 590, 1100, 700, "#FFE4E6"),
        "地图要素": (1180, 590, 1440, 700, "#E5E7EB"),
        "质量约束": (770, 820, 1030, 930, "#DCFCE7"),
    }
    centers = {}
    for text, (x1, y1, x2, y2, fill) in nodes.items():
        draw_round_box(draw, (x1, y1, x2, y2), text, fill)
        centers[text] = ((x1 + x2) / 2, (y1 + y2) / 2)
    edges = [
        ("制图任务", "行动主体"), ("制图任务", "时间信息"), ("制图任务", "路线节点"), ("制图任务", "历史事件"),
        ("路线节点", "路线关系"), ("路线节点", "表达方法"), ("历史事件", "地图要素"),
        ("路线关系", "质量约束"), ("表达方法", "质量约束"), ("地图要素", "质量约束"),
        ("历史事件", "路线节点"), ("表达方法", "地图要素")
    ]
    for a, b in edges:
        arrow(draw, centers[a], centers[b], "#4B5563", 3)
    draw.text((80, 965), "本体作用：限定抽取字段、约束路线组织、支撑工具选择，并为成果质量评价提供检查依据。", fill="#374151", font=small_font)
    img.save(FIG_DIR / "ontology.png")

    # Figure 3: Multi-agent collaboration
    img, draw = save_canvas(FIG_DIR / "agents.png", 1800, 1050)
    draw.text((60, 38), "知识约束下的多智能体协同机制", fill="#111827", font=title_font)
    center = (760, 430, 1040, 570)
    draw_round_box(draw, center, "中央协调智能体\n任务状态与流程控制", "#DBEAFE")
    agents = [
        ((180, 190, 470, 320), "意图解析智能体\n制图目标、范围、成果形式", "#ECFDF5"),
        ((180, 610, 470, 740), "数据推理智能体\n图层、坐标、字段、底图检查", "#FEEFCA"),
        ((760, 780, 1040, 910), "路线表达智能体\n节点连接、路线符号、事件表达", "#FCE7F3"),
        ((1330, 610, 1620, 740), "版式编排智能体\n标题、图例、比例尺、说明框", "#EDE9FE"),
        ((1330, 190, 1620, 320), "质量评价智能体\n连贯性、完整性、可读性", "#FEE2E2"),
    ]
    for box, text, fill in agents:
        draw_round_box(draw, box, text, fill, font_size=22)
        cx = (box[0] + box[2]) / 2
        cy = (box[1] + box[3]) / 2
        arrow(draw, (900, 500), (cx, cy), "#4B5563", 3)
        arrow(draw, (cx, cy), (900, 500), "#9CA3AF", 2)
    draw_round_box(draw, (760, 170, 1040, 300), "路线制图知识图谱\n共享语义状态", "#DCFCE7", font_size=22)
    arrow(draw, (900, 300), (900, 430), "#16A34A", 4)
    draw_round_box(draw, (760, 610, 1040, 720), "QGIS工具契约\n输入 输出 失败反馈", "#E5E7EB", font_size=22)
    arrow(draw, (900, 570), (900, 610), "#374151", 4)
    img.save(FIG_DIR / "agents.png")

    # Figure 4: Execution sequence
    img, draw = save_canvas(FIG_DIR / "workflow.png", 1800, 900)
    draw.text((60, 38), "从路线文本到地图成果的闭环执行流程", fill="#111827", font=title_font)
    steps = [
        "输入路线文本", "抽取节点事件", "构建知识图谱", "生成制图计划", "调用QGIS工具", "导出地图成果", "质量评价", "反馈修订"
    ]
    x = 80
    y = 260
    for idx, step in enumerate(steps):
        box = (x, y, x + 180, y + 120)
        draw_round_box(draw, box, f"{idx+1}\n{step}", "#EFF6FF" if idx < 6 else "#FEF2F2", font_size=22)
        if idx < len(steps) - 1:
            arrow(draw, (x + 180, y + 60), (x + 235, y + 60))
        x += 220
    draw.line((1530, 380, 1530, 610, 300, 610, 300, 380), fill="#B91C1C", width=4)
    arrow(draw, (300, 610), (300, 390), "#B91C1C", 4)
    draw.text((460, 660), "反馈修订不是附加说明，而是将评价结果重新转化为可执行的符号、标注和版式调整操作。", fill="#374151", font=small_font)
    img.save(FIG_DIR / "workflow.png")

    # Figure 5: Route map schematic
    img, draw = save_canvas(FIG_DIR / "route_case.png", 1800, 1060)
    draw.text((60, 38), "行军路线图案例成果示意", fill="#111827", font=title_font)
    map_box = (80, 140, 1320, 930)
    draw.rounded_rectangle(map_box, radius=20, fill="#F8FAFC", outline="#CBD5E1", width=3)
    pts = [(250, 720, "遵义"), (520, 610, "桐梓"), (770, 500, "娄山关"), (1060, 670, "赤水")]
    for i in range(len(pts) - 1):
        draw.line([(pts[i][0], pts[i][1]), (pts[i + 1][0], pts[i + 1][1])], fill="#DC2626", width=10)
        arrow(draw, (pts[i][0], pts[i][1]), (pts[i + 1][0], pts[i + 1][1]), "#DC2626", 6)
    for x, y, label in pts:
        draw.ellipse((x - 16, y - 16, x + 16, y + 16), fill="#1D4ED8", outline="#FFFFFF", width=4)
        draw.text((x - 35, y + 24), label, fill="#111827", font=get_font(24))
    draw.rounded_rectangle((730, 270, 1130, 390), radius=12, fill="#FFF7ED", outline="#FB923C", width=3)
    draw.text((760, 298), "事件说明：娄山关战斗\n与娄山关节点关联", fill="#7C2D12", font=get_font(24))
    draw.line((850, 390, 780, 500), fill="#FB923C", width=3)
    draw.rectangle((1120, 160, 1290, 280), fill="#FFFFFF", outline="#CBD5E1", width=2)
    draw.text((1140, 180), "图例", fill="#111827", font=get_font(24))
    draw.line((1140, 235, 1200, 235), fill="#DC2626", width=8)
    draw.text((1210, 220), "主路线", fill="#111827", font=get_font(22))
    draw.ellipse((1148, 252, 1172, 276), fill="#1D4ED8")
    draw.text((1210, 250), "关键节点", fill="#111827", font=get_font(22))
    draw.text((1400, 180), "质量检查要点", fill="#111827", font=get_font(28))
    checks = ["节点是否完整", "路线顺序是否正确", "事件与地点是否匹配", "图例是否覆盖符号", "说明框是否遮挡路线"]
    yy = 245
    for c in checks:
        draw.text((1400, yy), f"□ {c}", fill="#374151", font=get_font(24))
        yy += 62
    img.save(FIG_DIR / "route_case.png")


def set_run_font(run, size=10.5, bold=False, color=None):
    run.font.name = "Times New Roman"
    run._element.rPr.rFonts.set(qn("w:eastAsia"), "宋体")
    run.font.size = Pt(size)
    run.bold = bold
    if color:
        run.font.color.rgb = RGBColor(*color)


def set_para(p, first_line=True, after=6, line=1.25):
    p.paragraph_format.space_before = Pt(0)
    p.paragraph_format.space_after = Pt(after)
    p.paragraph_format.line_spacing = line
    if first_line:
        p.paragraph_format.first_line_indent = Cm(0.74)


def add_para(doc, text="", first_line=True, after=6):
    p = doc.add_paragraph()
    set_para(p, first_line=first_line, after=after)
    if text:
        r = p.add_run(text)
        set_run_font(r)
    return p


def add_mixed(doc, parts, first_line=True, after=6):
    p = doc.add_paragraph()
    set_para(p, first_line=first_line, after=after)
    for text, kind in parts:
        r = p.add_run(text)
        set_run_font(r, color=(192, 0, 0) if kind == "red" else None, bold=(kind == "bold"))
    return p


def add_heading(doc, text, level=1):
    p = doc.add_paragraph()
    p.style = f"Heading {level}"
    p.paragraph_format.space_before = Pt(10 if level == 1 else 6)
    p.paragraph_format.space_after = Pt(6 if level == 1 else 4)
    p.paragraph_format.line_spacing = 1.2
    r = p.add_run(text)
    set_run_font(r, size=14 if level == 1 else 12, bold=True)
    return p


def add_figure(doc, image_name, caption):
    p = doc.add_paragraph()
    p.alignment = WD_ALIGN_PARAGRAPH.CENTER
    p.paragraph_format.space_before = Pt(6)
    p.paragraph_format.space_after = Pt(2)
    p.add_run().add_picture(str(FIG_DIR / image_name), width=Cm(15.2))
    cap = doc.add_paragraph()
    cap.alignment = WD_ALIGN_PARAGRAPH.CENTER
    cap.paragraph_format.space_after = Pt(8)
    r = cap.add_run(caption)
    set_run_font(r, size=9)


def shade_cell(cell, fill):
    tc_pr = cell._tc.get_or_add_tcPr()
    shd = OxmlElement("w:shd")
    shd.set(qn("w:fill"), fill)
    tc_pr.append(shd)


def set_table_borders(table):
    borders = table._tbl.tblPr.first_child_found_in("w:tblBorders")
    if borders is None:
        borders = OxmlElement("w:tblBorders")
        table._tbl.tblPr.append(borders)
    for edge in ("top", "left", "bottom", "right", "insideH", "insideV"):
        tag = "w:" + edge
        el = borders.find(qn(tag))
        if el is None:
            el = OxmlElement(tag)
            borders.append(el)
        el.set(qn("w:val"), "single")
        el.set(qn("w:sz"), "4")
        el.set(qn("w:space"), "0")
        el.set(qn("w:color"), "BFBFBF")


def set_cell(cell, text, bold=False, red=False):
    cell.text = ""
    p = cell.paragraphs[0]
    p.paragraph_format.space_after = Pt(0)
    p.paragraph_format.line_spacing = 1.15
    p.alignment = WD_ALIGN_PARAGRAPH.CENTER if len(text) <= 10 else WD_ALIGN_PARAGRAPH.LEFT
    r = p.add_run(text)
    set_run_font(r, size=9, bold=bold, color=(192, 0, 0) if red else None)
    cell.vertical_alignment = WD_CELL_VERTICAL_ALIGNMENT.CENTER


def add_table(doc, headers, rows, caption):
    table = doc.add_table(rows=len(rows) + 1, cols=len(headers))
    table.alignment = WD_TABLE_ALIGNMENT.CENTER
    set_table_borders(table)
    for i, h in enumerate(headers):
        shade_cell(table.cell(0, i), "F2F2F2")
        set_cell(table.cell(0, i), h, bold=True)
    for r_idx, row in enumerate(rows, start=1):
        for c_idx, val in enumerate(row):
            set_cell(table.cell(r_idx, c_idx), val, red=(val == "待补充"))
    cap = doc.add_paragraph()
    cap.alignment = WD_ALIGN_PARAGRAPH.CENTER
    cap.paragraph_format.space_after = Pt(8)
    r = cap.add_run(caption)
    set_run_font(r, size=9)


def build_doc():
    make_figures()
    doc = Document()
    sec = doc.sections[0]
    sec.top_margin = Cm(2.5)
    sec.bottom_margin = Cm(2.5)
    sec.left_margin = Cm(2.8)
    sec.right_margin = Cm(2.8)

    normal = doc.styles["Normal"]
    normal.font.name = "Times New Roman"
    normal._element.rPr.rFonts.set(qn("w:eastAsia"), "宋体")
    normal.font.size = Pt(10.5)

    title = doc.add_paragraph()
    title.alignment = WD_ALIGN_PARAGRAPH.CENTER
    title.paragraph_format.space_after = Pt(10)
    r = title.add_run("本体约束与多智能体协同的路线类专题图自动制图方法")
    set_run_font(r, size=16, bold=True)

    p = add_para(doc, first_line=False, after=4)
    r = p.add_run("摘要：")
    set_run_font(r, bold=True)
    r = p.add_run("路线类专题图需要表达地点顺序、事件过程、路线结构和地图版式，是专题图制图中语义组织较强、流程链条较长的一类任务。传统GIS制图依赖人工阅读材料、整理路线关系和配置地图要素，难以直接响应自然语言和历史叙事文本中的制图意图。针对非结构化路线文本难以转化为可执行制图知识、通用GIS智能体缺少路线语义约束以及地图生成结果缺少反馈修订的问题，提出一种本体约束与多智能体协同的路线类专题图自动制图方法。该方法构建包含制图任务、路线节点、历史事件、路线关系、表达方法、地图要素和质量约束的路线制图本体，在本体约束下抽取时间、地点、事件和表达要求，并组织为路线制图知识图谱；进一步设计意图解析、数据推理、路线表达、版式编排和质量评价等任务型智能体，通过工具契约驱动QGIS完成路线图生成；最后结合图谱关系、图层状态和地图图像开展质量评价，将评价结果反馈为可执行的符号、标注和版式修订指令。以行军路线图为例的原型实验表明，该方法能够完成路线节点抽取、事件地点匹配、路线表达、版式生成和成果导出，初步验证了自然语言驱动路线类专题图闭环制图的可行性。")
    set_run_font(r)

    p = add_para(doc, first_line=False, after=10)
    r = p.add_run("关键词：")
    set_run_font(r, bold=True)
    r = p.add_run("路线类专题图；本体约束；知识图谱；多智能体；自动制图；QGIS")
    set_run_font(r)

    add_heading(doc, "1 引言")
    intro = [
        "专题图是表达地理现象空间分布、专题属性差异和时空过程关系的重要地图类型，在历史研究、国土空间规划、交通组织、应急救援和军事推演等场景中具有广泛应用。随着大语言模型和智能体技术的发展，GIS操作方式正在从菜单命令、参数面板和脚本编程逐步转向自然语言驱动。用户可以用接近日常表达的方式描述空间分析或地图制作需求，智能体则负责理解任务、组织流程、调用工具并返回结果。这一趋势为降低GIS使用门槛和提升专题图制作效率提供了新的技术路径。",
        "路线类专题图是专题图中较为典型且复杂的一类。与普通点状分布图或分级设色图相比，路线类专题图不仅需要表达空间位置，还需要表达行动主体、时间顺序、地点连接、事件发生、路线分支和节点说明等语义关系。以行军路线图为例，原始材料往往以历史叙事或业务说明形式出现，其中地点、时间、事件和制图要求分散在自然语言文本中。制图人员需要先阅读文本、识别关键节点、判断路线顺序、匹配事件地点，再在GIS软件中绘制路线、配置符号、添加注记和组织版式。该过程具有明显的知识密集性和人工经验依赖。",
        "已有大语言模型辅助GIS研究已经证明自然语言驱动空间处理和制图具有可行性。相关系统能够根据用户请求调用数据加载、空间分析、符号化和地图输出工具，也有研究通过多智能体机制完成地图风格迁移、行政区划图配色和空间数据处理任务。然而，路线类专题图自动制图仍面临三方面问题。第一，非结构化路线文本难以直接转化为可执行制图知识。通用智能体通常擅长处理明确命令，但对文本中的节点顺序、事件归属和隐含路线关系缺少稳定抽取能力。第二，通用工具调用缺少路线类专题图的语义约束，容易出现节点连接错误、事件注记缺失、表达方法不一致等问题。第三，地图生成后缺少面向路线连贯性、节点完整性、图例一致性和版式可读性的反馈修订机制。",
        "因此，路线类专题图自动制图不能简单理解为让模型调用绘线工具，而应构建从文本叙事到制图知识、从制图知识到工具执行、从地图成果到质量反馈的完整链条。本文提出一种本体约束与多智能体协同的路线类专题图自动制图方法。该方法将路线制图本体作为语义约束，将知识图谱作为中间表示，将多智能体作为任务组织机制，将QGIS工具链作为本地执行基础，并通过质量评价实现闭环修订。总体框架如图1所示。"
    ]
    for t in intro:
        add_para(doc, t)
    add_figure(doc, "framework.png", "图1 路线类专题图自动制图总体框架")
    add_para(doc, "本文的主要贡献包括以下三点。第一，提出面向路线类专题图的本体约束信息抽取方法，将自然语言文本中的时间、地点、事件、路线节点和表达要求组织为结构化制图语义。第二，构建路线制图知识图谱和任务型多智能体协同机制，使智能体规划不再直接依赖原始文本，而是基于显式路线关系和制图约束进行工具选择。第三，设计面向路线类专题图的质量评价与反馈修订流程，将路线连贯性、节点完整性、事件匹配、图例一致性和版式可读性等评价结果转化为可执行修订操作。")

    add_heading(doc, "2 相关研究")
    add_para(doc, "大语言模型在自然语言理解、任务分解和代码生成方面的能力推动了智能GIS研究。AutonomousGIS提出以大语言模型作为GIS推理核心，探索自动完成空间数据获取、分析和可视化任务。ChatGeoAI等研究将自然语言请求转化为PyQGIS代码，使非专业用户能够通过对话方式开展空间分析。Smart-QGIS、MapGPT等工作进一步将大语言模型与QGIS或制图工具集成，使模型能够根据自然语言调用数据加载、空间处理、图层渲染和地图输出功能。这类研究证明了自然语言驱动GIS工具调用的可行性，但重点通常在工具链打通和通用空间处理，对专题图制图中的语义结构、表达规范和反馈修订关注不足。")
    add_para(doc, "多智能体方法通过角色分工和任务分解提升复杂任务处理能力。ShapefileGPT采用规划智能体和执行智能体协同处理Shapefile任务，说明多智能体结构能够增强空间数据处理的稳定性。CartoAgent将地图制作划分为准备、设计和评价阶段，利用多模态大模型完成地图风格迁移和评价。上述研究表明，多智能体机制适合处理包含多个阶段的制图或空间处理任务。但现有工作多面向通用空间数据处理、地图风格迁移或配色设计，对路线类专题图中的节点顺序、事件地点、分支关系和版式组织缺少专门设计。")
    add_para(doc, "知识图谱和本体建模为领域知识约束提供了有效方式。知识图谱能够显式表达实体、关系和规则，有助于弥补大语言模型在事实一致性、领域推理和结构化约束方面的不足。在自动制图任务中，制图知识不仅包括地理实体和空间关系，还包括表达方法、视觉变量、图例配置、注记规则和质量标准。路线类专题图尤其需要表达时间、地点、事件和路线连接之间的复合关系。本文将路线制图本体引入智能体制图过程，目的在于把文本中的路线叙事转化为可查询、可约束、可反馈的制图知识。")

    add_heading(doc, "3 本体约束的路线制图知识组织")
    add_para(doc, "路线类专题图的自动生成首先需要解决语义组织问题。自然语言文本中的“从某地出发，经某地到达某地”“某事件发生于某地”“需要突出主路线并标注关键节点”等表述，不能直接作为GIS绘图参数。系统需要识别其中的行动主体、时间信息、地点实体、路线节点、历史事件、路线关系和表达要求，并将其转化为具有明确字段和关系的制图知识。")
    add_para(doc, "本文构建的路线制图本体包含七类核心概念。制图任务描述地图类型、主题和成果形式。路线节点表示路线经过地点及其顺序属性。历史事件表示战斗、会师、转移、驻留等与路线相关的事件。路线关系描述节点之间的先后连接、分支汇入、多起点汇合和回环关系。表达方法描述路线线型、颜色、线宽、箭头、节点符号、标注等级和说明框形式。地图要素包括标题、图例、比例尺、指北针、注记、说明框和导出格式。质量约束用于描述路线连贯性、节点完整性、事件匹配、标注可读性和版式完整性等评价要求。核心关系如图2所示。")
    add_figure(doc, "ontology.png", "图2 路线类专题图本体与知识图谱核心关系")
    add_para(doc, "在本体约束下，信息抽取不再是开放式文本摘要，而是面向制图任务的结构化识别。地点实体需要记录名称、别名、行政区划线索和是否需要地理编码；路线节点需要记录节点序号、节点类型、重要程度和标注等级；历史事件需要记录事件名称、发生时间、关联节点和说明文本；表达要求需要记录主路线强调、节点标注、事件说明、图例项和输出格式等信息。抽取结果经过字段完整性、节点顺序和事件地点匹配检查后进入知识图谱。")
    add_table(doc,
              ["概念", "含义", "主要属性", "制图作用"],
              [
                  ["制图任务", "用户提出的路线图制作目标", "主题、范围、输出格式", "确定任务边界"],
                  ["路线节点", "路线经过的关键地点", "名称、序号、等级、坐标", "生成路线和标注"],
                  ["历史事件", "与路线节点相关的事件", "名称、时间、地点、说明", "生成事件注记"],
                  ["路线关系", "节点之间的连接关系", "起点、终点、类型、顺序", "控制路线组织"],
                  ["表达方法", "路线和节点的视觉表达", "线型、颜色、符号、标注", "控制专题符号"],
                  ["质量约束", "成果检查标准", "完整性、连贯性、可读性", "支撑反馈修订"],
              ],
              "表1 路线制图本体核心概念")
    add_para(doc, "知识图谱在系统中发挥三类作用。首先，它作为信息抽取的字段约束，使模型围绕路线制图任务输出结果。其次，它作为智能体规划的共享状态，使路线表达和版式编排基于节点关系和事件关系，而不是基于模型对原文的临时理解。最后，它作为质量评价的检查依据，使系统能够判断地图成果是否完整表达了图谱中的节点、事件、路线和地图要素。")

    add_heading(doc, "4 多智能体协同与工具执行方法")
    add_para(doc, "路线类专题图制图是一个长流程任务，单一智能体容易在需求理解、数据检查、路线组织、符号配置和质量评价之间发生职责混淆。本文采用任务型多智能体机制，将路线图制作过程划分为相互衔接的子任务。中央协调智能体维护任务状态和执行顺序；意图解析智能体识别制图目标、路线范围和成果形式；数据推理智能体检查工程图层、坐标系统、字段名称和底图可用性；路线表达智能体依据知识图谱生成主路线、分支路线和事件节点表达方案；版式编排智能体组织标题、图例、比例尺、指北针、节点标注和说明框；质量评价智能体负责检查成果并生成修订建议。协同结构如图3所示。")
    add_figure(doc, "agents.png", "图3 知识约束下的多智能体协同机制")
    add_para(doc, "与仅依靠对话文本传递信息不同，各智能体之间共享结构化任务状态，包括抽取结果、图谱关系、图层状态、工具输出和质量评价结果。这样可以减少长上下文对模型记忆的依赖，并使每个阶段的输入和输出可检查。比如，路线表达智能体不需要重新阅读整段历史文本，而是直接读取图谱中的节点顺序和路线关系；版式编排智能体不需要猜测地图要素，而是根据图谱和质量约束配置标题、图例和说明框。")
    add_para(doc, "工具执行层以QGIS和PyQGIS为本地制图环境。系统没有直接要求模型生成完整脚本，而是将路线图制作拆解为数据加载、节点检查、路线段构建、符号配置、标注生成、图例配置、布局创建和成果导出等标准化工具调用。每个工具均通过工具契约限定参数、返回值、适用条件和失败反馈。该方式既保留了QGIS对空间数据、图层样式和布局对象的可编辑能力，也降低了大语言模型一次性生成完整脚本带来的语法错误和参数缺失风险。闭环执行流程如图4所示。")
    add_figure(doc, "workflow.png", "图4 从路线文本到地图成果的闭环执行流程")
    add_table(doc,
              ["阶段", "输入", "核心操作", "输出"],
              [
                  ["文本解析", "路线叙事文本", "抽取时间、地点、事件和表达要求", "结构化制图需求"],
                  ["知识组织", "结构化需求", "构建节点、事件和路线关系", "路线制图知识图谱"],
                  ["任务规划", "知识图谱和工程状态", "分解任务并选择工具", "工具调用序列"],
                  ["制图执行", "工具参数和空间数据", "生成图层、样式和布局", "地图成果与日志"],
                  ["质量反馈", "地图图像和图谱关系", "检查问题并生成修订指令", "局部修订结果"],
              ],
              "表2 路线类专题图自动制图流程")

    add_heading(doc, "5 原型实现与案例验证")
    add_para(doc, "为验证方法可行性，本文基于本地QGIS环境实现路线类专题图制图原型，并以行军路线图作为典型案例进行验证。实验并不追求覆盖全部GIS任务，而是重点检验从自然语言路线文本到地图成果输出的完整链条，包括文本解析、知识组织、智能体工具调用、QGIS地图生成和质量反馈修订。由于项目仍在完善中，批量实验数据暂不填入，相关数值以红色待补充标明，后续可在系统稳定后直接替换。")
    add_para(doc, "案例输入文本如下。")
    q = add_para(doc, "1935年1月，中央红军从遵义出发，经桐梓、娄山关等地向黔北转移。2月25日，红军攻占娄山关，随后二渡赤水，回师黔北。制图时需要突出主行军路线，标注遵义、桐梓、娄山关和赤水等关键节点，并对娄山关战斗进行说明。")
    q.paragraph_format.left_indent = Cm(0.74)
    add_para(doc, "系统首先识别出行动主体为中央红军，时间信息包括1935年1月和2月25日，路线节点包括遵义、桐梓、娄山关和赤水，历史事件为娄山关战斗，表达要求包括突出主行军路线、标注关键节点和配置事件说明。随后，知识图谱将遵义、桐梓、娄山关和赤水组织为先后连接关系，将娄山关战斗关联到娄山关节点，并将主路线强调、节点标注和事件说明映射到具体制图表达方法。")
    add_para(doc, "在制图执行阶段，智能体根据图谱关系生成路线段和关键节点图层，并在QGIS中配置主路线符号、节点符号、事件说明框、图例、标题、比例尺和指北针等地图要素。图5给出了案例成果示意。该图不是最终实测地图截图，而是用于论文中说明原型系统成果结构的示意图；后续完成稳定案例后，可替换为真实导出地图。")
    add_figure(doc, "route_case.png", "图5 行军路线图案例成果示意")
    add_mixed(doc, [
        ("当前案例中，路线节点抽取完整率为", None),
        ("待补充", "red"),
        ("，事件地点匹配正确率为", None),
        ("待补充", "red"),
        ("，地图成果导出成功率为", None),
        ("待补充", "red"),
        ("。质量评价模块从路线连贯性、节点完整性、事件标注准确性、符号层次、图例一致性和版式完整性等维度检查结果，并将问题反馈给路线表达智能体和版式编排智能体。修订前地图质量评分为", None),
        ("待补充", "red"),
        ("，修订后地图质量评分为", None),
        ("待补充", "red"),
        ("。", None),
    ])
    add_table(doc,
              ["指标", "说明", "结果"],
              [
                  ["节点抽取完整率", "正确识别的路线节点数量占应识别节点数量比例", "待补充"],
                  ["节点顺序正确率", "路线节点连接顺序与文本叙事一致的比例", "待补充"],
                  ["事件地点匹配率", "事件与发生地点正确关联的比例", "待补充"],
                  ["工具调用成功率", "成功执行工具数占全部工具调用数比例", "待补充"],
                  ["人工干预次数", "完成一次制图任务所需人工修正次数", "待补充"],
                  ["质量评分提升", "反馈修订前后地图质量评分变化", "待补充"],
              ],
              "表3 案例实验评价指标与待补充结果")
    add_para(doc, "从原型流程看，本文方法的优势主要体现在过程可控性。直接脚本生成方法通常需要模型一次性给出完整PyQGIS代码，若字段、图层或布局状态不符合预期，错误定位和局部修订较困难。本文方法通过知识图谱和工具契约把任务拆分为可追踪步骤，使系统能够在节点抽取、路线构建、符号配置和版式导出等环节分别检查状态并进行修订。")

    add_heading(doc, "6 讨论")
    add_para(doc, "路线类专题图自动制图的核心难点在于语义结构和制图执行之间的转换。路线文本中的地点顺序、事件归属和表达要求具有明显的上下文依赖，如果直接由模型生成工具调用，容易受到提示词、历史对话和工具列表干扰。本文引入本体和知识图谱的目的，是在模型自由推理和GIS确定性执行之间增加一层结构化约束，使智能体能够围绕明确的路线节点、事件关系和地图要素进行规划。")
    add_para(doc, "多智能体机制的价值不在于简单增加角色数量，而在于将不同类型的问题分开处理。意图解析关注文本语义，数据推理关注工程状态，路线表达关注节点和线状符号，版式编排关注地图要素组织，质量评价关注成果检查。这样的分工符合路线类专题图的实际制作流程，也便于后续定位错误来源。例如，如果节点遗漏，问题主要发生在文本抽取或地名识别阶段；如果路线顺序错误，问题主要发生在图谱关系构建或路线表达阶段；如果标注遮挡，问题则属于版式编排和质量反馈阶段。")
    add_para(doc, "本文仍存在局限。第一，当前实验以原型案例为主，尚需补充多样本任务和量化统计结果。第二，路线制图本体主要围绕行军路线图构建，对历史迁徙图、交通规划图、应急救援路线图等场景还需扩展领域概念和规则。第三，质量评价中的视觉层次、标注遮挡和版式审美仍部分依赖多模态模型判断，其稳定性需要进一步验证。第四，地名消歧和坐标匹配在复杂历史地名场景中仍可能影响路线图生成结果。")

    add_heading(doc, "7 结论")
    add_para(doc, "本文提出一种本体约束与多智能体协同的路线类专题图自动制图方法。该方法面向路线图制图中非结构化文本难以直接驱动GIS工具、路线语义关系缺少约束、地图生成结果缺少反馈修订等问题，构建了路线制图本体和知识图谱，并设计任务型多智能体机制驱动QGIS工具链完成地图生成。与通用工具调用式智能体相比，本文方法强调在工具执行之前显式组织路线节点、历史事件、路线关系、表达方法和质量约束，使自然语言制图过程具有更强的可解释性和可追踪性。")
    add_para(doc, "行军路线图案例验证表明，该方法能够将路线叙事文本转化为结构化制图知识，并支持路线表达、节点标注、事件说明和版式导出等关键环节。后续工作将补充批量实验数据，完善不同路线类专题图场景下的本体规则，增强地名消歧和多源数据匹配能力，并进一步提高质量评价结果向可执行修订指令转化的稳定性。")

    add_heading(doc, "参考文献")
    refs = [
        "[1] Li Z, Ning H, Gao S, et al. Autonomous GIS: the next-generation AI-powered GIS[J]. International Journal of Digital Earth, 2023.",
        "[2] Mansourian A, Oucheikh R. ChatGeoAI: Enabling geospatial analysis for public through natural language, with large language models[J]. ISPRS International Journal of Geo-Information, 2024, 13(10): 348.",
        "[3] 谭振宇, 杨安平, 马镇翼, 高美玲, 余琛. Smart-QGIS: 空间数据处理与制图智能体[J]. 地球信息科学学报, 2026, 28(4): 1007-1019.",
        "[4] Zhang Y, He Z, Li J, Lin J, Guan Q, Yu W. MapGPT: an autonomous framework for mapping by integrating large language model and cartographic tools[J]. Cartography and Geographic Information Science, 2024.",
        "[5] Lin Q, Hu R, Li H, et al. ShapefileGPT: A multi-agent large language model framework for automated shapefile processing[EB/OL]. arXiv, 2024.",
        "[6] Wang C, Kang Y, Gong Z, et al. CartoAgent: a multimodal large language model-powered multi-agent cartographic framework for map style transfer and evaluation[EB/OL]. arXiv, 2025.",
        "[7] Pan J Z, Razniewski S, Kalo J C, et al. Large Language Models and Knowledge Graphs: Opportunities and Challenges[EB/OL]. arXiv, 2023.",
        "[8] Xu J, Tao R. Map reading and analysis with GPT-4V(ision)[J]. ISPRS International Journal of Geo-Information, 2024, 13(4): 127."
    ]
    for ref in refs:
        p = doc.add_paragraph()
        set_para(p, first_line=False, after=3, line=1.15)
        r = p.add_run(ref)
        set_run_font(r, size=9.5)

    doc.save(OUT)
    print(OUT)


if __name__ == "__main__":
    build_doc()
