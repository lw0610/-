from pathlib import Path
from typing import Iterable

from docx import Document
from docx.enum.text import WD_ALIGN_PARAGRAPH
from docx.oxml import OxmlElement
from docx.shared import Cm, Pt
from docx.text.paragraph import Paragraph
from PIL import Image, ImageDraw, ImageFont


ROOT = Path("/Users/zw/研究生/QGIS-agent/Eino")
SRC = Path("/Users/zw/Downloads/AutoCarto_专题图制图智能体论文-修订模式.docx")
OUT = ROOT / "AutoCarto_专题图制图智能体论文_系统架构与方法重写版.docx"
FIG_DIR = ROOT / "docs" / "paper_figures"
FIG_PNG = FIG_DIR / "system_method_academic_framework.png"
FIG_SVG = FIG_DIR / "system_method_academic_framework.svg"

W, H = 2600, 1600


def font(size: int):
    candidates = [
        "/System/Library/Fonts/Hiragino Sans GB.ttc",
        "/System/Library/Fonts/STHeiti Medium.ttc",
        "/System/Library/Fonts/Supplemental/Songti.ttc",
    ]
    for candidate in candidates:
        if Path(candidate).exists():
            return ImageFont.truetype(candidate, size=size)
    return ImageFont.load_default()


def text_size(draw: ImageDraw.ImageDraw, text: str, fnt) -> tuple[int, int]:
    box = draw.textbbox((0, 0), text, font=fnt)
    return box[2] - box[0], box[3] - box[1]


def center_text(draw, x1, y, x2, text, fnt, fill="#1E293B"):
    tw, _ = text_size(draw, text, fnt)
    draw.text((x1 + (x2 - x1 - tw) / 2, y), text, font=fnt, fill=fill)


def wrap_cn(draw, text, fnt, width_px):
    lines, cur = [], ""
    for ch in text:
        trial = cur + ch
        if text_size(draw, trial, fnt)[0] <= width_px:
            cur = trial
        else:
            if cur:
                lines.append(cur)
            cur = ch
    if cur:
        lines.append(cur)
    return lines


def arrow(draw, start, end, color="#64748B", width=3):
    import math

    draw.line([start, end], fill=color, width=width)
    angle = math.atan2(end[1] - start[1], end[0] - start[0])
    size = 14
    p1 = (end[0] - size * math.cos(angle - math.pi / 6), end[1] - size * math.sin(angle - math.pi / 6))
    p2 = (end[0] - size * math.cos(angle + math.pi / 6), end[1] - size * math.sin(angle + math.pi / 6))
    draw.polygon([end, p1, p2], fill=color)


def panel(draw, xy, label, title):
    x1, y1, x2, y2 = xy
    draw.rounded_rectangle(xy, radius=22, fill="#FFFFFF", outline="#CBD5E1", width=2)
    draw.text((x1 + 26, y1 + 22), label, font=font(32), fill="#0F172A")
    draw.text((x1 + 83, y1 + 25), title, font=font(27), fill="#1E293B")


def small_box(draw, xy, title, body="", fill="#F8FAFC", outline="#CBD5E1", title_size=22, body_size=17):
    x1, y1, x2, y2 = xy
    draw.rounded_rectangle(xy, radius=14, fill=fill, outline=outline, width=2)
    center_text(draw, x1 + 8, y1 + 18, x2 - 8, title, font(title_size), "#0F172A")
    if body:
        yy = y1 + 52
        for line in wrap_cn(draw, body, font(body_size), x2 - x1 - 30):
            center_text(draw, x1 + 14, yy, x2 - 14, line, font(body_size), "#475569")
            yy += body_size + 8


def pill(draw, xy, text, fill="#FFFFFF", outline="#CBD5E1", text_color="#334155"):
    x1, y1, x2, y2 = xy
    draw.rounded_rectangle(xy, radius=(y2 - y1) // 2, fill=fill, outline=outline, width=1)
    center_text(draw, x1, y1 + 10, x2, text, font(18), text_color)


def ellipse(draw, cx, cy, rx, ry, text, fill="#F8FAFC", outline="#94A3B8"):
    draw.ellipse((cx - rx, cy - ry, cx + rx, cy + ry), fill=fill, outline=outline, width=2)
    center_text(draw, cx - rx, cy - 13, cx + rx, text, font(20), "#0F172A")


def make_png():
    FIG_DIR.mkdir(parents=True, exist_ok=True)
    img = Image.new("RGB", (W, H), "white")
    d = ImageDraw.Draw(img)
    d.text((90, 52), "知识约束的路线类专题图智能制图方法", font=font(44), fill="#0F172A")
    d.text((90, 110), "从自然语言路线叙事到可验证地图成果的语义表示、智能体协同与 GIS 执行机制", font=font(24), fill="#64748B")

    # Panel a: layered architecture.
    panel(d, (75, 185, 1115, 825), "a", "系统层级与信息状态")
    layer_specs = [
        ("用户交互层", "路线需求、文本材料、地图预览", "#EAF4FB", "需求"),
        ("Go 后端调度层", "会话状态、任务队列、工具网关", "#EEF3F7", "调度"),
        ("多智能体层", "意图解析、数据推理、路线表达、版式评价", "#F4F7FA", "推理"),
        ("QGIS 执行层", "图层、符号、标注、布局与导出", "#E8F3F1", "执行"),
    ]
    y = 275
    for i, (title, body, fill, side) in enumerate(layer_specs):
        yy = y + i * 122
        pill(d, (120, yy + 18, 245, yy + 68), side, "#FFFFFF", "#CBD5E1")
        small_box(d, (285, yy, 1035, yy + 88), title, body, fill, "#B6C9D8", 24, 18)
        if i < len(layer_specs) - 1:
            arrow(d, (660, yy + 88), (660, yy + 116), "#94A3B8", 3)
    pill(d, (360, 760, 960, 805), "输出：结构化制图状态、QGIS 工程对象、专题图成果与运行日志", "#F8FAFC")

    # Panel b: representation and ontology graph.
    panel(d, (1180, 185, 2525, 825), "b", "路线制图语义表示与知识约束")
    nodes = {
        "制图任务": (1845, 505, 110, 45, "#E0F2FE"),
        "路线节点": (1565, 350, 95, 38, "#ECFDF5"),
        "路线段": (2125, 350, 82, 38, "#ECFDF5"),
        "历史事件": (1530, 635, 95, 38, "#FEF3C7"),
        "空间位置": (1845, 710, 95, 38, "#F8FAFC"),
        "表达方法": (2160, 635, 95, 38, "#FCE7F3"),
        "地图要素": (1845, 275, 95, 38, "#EDE9FE"),
        "质量约束": (2335, 505, 95, 38, "#FFE4E6"),
        "输出成果": (1355, 505, 95, 38, "#E5E7EB"),
    }
    edges = [
        ("制图任务", "路线节点", "包含"),
        ("路线节点", "路线段", "连接"),
        ("路线节点", "历史事件", "承载"),
        ("历史事件", "空间位置", "发生于"),
        ("路线段", "表达方法", "采用"),
        ("表达方法", "地图要素", "生成"),
        ("制图任务", "质量约束", "受控"),
        ("制图任务", "输出成果", "产生"),
        ("地图要素", "制图任务", "服务"),
    ]
    for a, b, label in edges:
        ax, ay, *_ = nodes[a]
        bx, by, *_ = nodes[b]
        d.line([(ax, ay), (bx, by)], fill="#94A3B8", width=2)
        mx, my = (ax + bx) / 2, (ay + by) / 2
        pill(d, (mx - 42, my - 17, mx + 42, my + 17), label, "#FFFFFF", "#E2E8F0", "#64748B")
    for title, (cx, cy, rx, ry, fill) in nodes.items():
        ellipse(d, cx, cy, rx, ry, title, fill)
    pill(d, (1270, 750, 2425, 795), "约束作用：字段完整性、路线顺序、事件地点匹配、表达规则和工具输入契约", "#F8FAFC")

    # Panel c: agent-role matrix.
    panel(d, (75, 885, 1295, 1505), "c", "任务型多智能体协同机制")
    roles = [
        ("协调", "维护目标、状态与子任务顺序"),
        ("意图解析", "识别主体、范围、节点与成果形式"),
        ("数据推理", "检查图层、字段、坐标与工程状态"),
        ("路线表达", "生成主线、分支、节点与事件表达"),
        ("版式编排", "组织标题、图例、比例尺和说明框"),
        ("质量评价", "检查连贯性、完整性和可读性"),
    ]
    x0, y0, cell_w, cell_h = 135, 1000, 185, 112
    for i, (r, desc) in enumerate(roles):
        x = x0 + (i % 3) * (cell_w + 28)
        yy = y0 + (i // 3) * (cell_h + 55)
        small_box(d, (x, yy, x + cell_w, yy + cell_h), r, desc, "#F8FAFC", "#CBD5E1", 22, 16)
    artifact_x = 810
    artifacts = [
        ("结构化需求", "#EAF4FB"),
        ("图谱关系", "#FEF3C7"),
        ("工具调用序列", "#EDE9FE"),
        ("执行状态", "#E8F3F1"),
        ("修订建议", "#FFE4E6"),
    ]
    for i, (name, fill) in enumerate(artifacts):
        small_box(d, (artifact_x, 1000 + i * 82, 1195, 1052 + i * 82), name, "", fill, "#CBD5E1", 20, 14)
    d.text((810, 1415), "协同原则：角色分工处理不同知识类型，", font=font(18), fill="#475569")
    d.text((810, 1445), "模块间传递结构化状态并支持局部修订。", font=font(18), fill="#475569")

    # Panel d: execution and evaluation evidence.
    panel(d, (1365, 885, 2525, 1505), "d", "工具执行与质量评价证据链")
    tool_cols = [
        ("数据接入", "图层/字段/坐标"),
        ("路线构建", "节点顺序/路线段"),
        ("表达配置", "线型/箭头/标注"),
        ("版式导出", "标题/图例/PDF"),
    ]
    x = 1440
    for i, (title, body) in enumerate(tool_cols):
        xx = x + i * 250
        small_box(d, (xx, 995, xx + 205, 1095), title, body, "#F8FAFC", "#CBD5E1", 20, 16)
        if i < len(tool_cols) - 1:
            arrow(d, (xx + 205, 1045), (xx + 245, 1045), "#94A3B8", 2)
    d.text((1440, 1148), "质量评价证据", font=font(24), fill="#1E293B")
    dims = ["路线连贯性", "节点完整性", "事件匹配", "符号层次", "版式可读性"]
    sources = ["知识图谱", "QGIS状态", "地图图像"]
    table_x, table_y = 1440, 1190
    row_h, col_w = 48, 178
    for j, txt in enumerate(["评价维度"] + sources):
        d.rectangle((table_x + j * col_w, table_y, table_x + (j + 1) * col_w, table_y + row_h), fill="#F1F5F9", outline="#CBD5E1")
        center_text(d, table_x + j * col_w, table_y + 14, table_x + (j + 1) * col_w, txt, font(17), "#0F172A")
    marks = [
        ["●", "●", "○"],
        ["●", "●", "○"],
        ["●", "○", "●"],
        ["○", "●", "●"],
        ["○", "●", "●"],
    ]
    for i, dim in enumerate(dims):
        yy = table_y + (i + 1) * row_h
        d.rectangle((table_x, yy, table_x + col_w, yy + row_h), fill="#FFFFFF", outline="#CBD5E1")
        center_text(d, table_x, yy + 14, table_x + col_w, dim, font(17), "#334155")
        for j, mark in enumerate(marks[i]):
            xx = table_x + (j + 1) * col_w
            d.rectangle((xx, yy, xx + col_w, yy + row_h), fill="#FFFFFF", outline="#CBD5E1")
            center_text(d, xx, yy + 9, xx + col_w, mark, font(25), "#0F766E" if mark == "●" else "#94A3B8")
    d.text((1445, 1480), "● 主要证据源    ○ 辅助证据源", font=font(17), fill="#64748B")

    img.save(FIG_PNG, dpi=(300, 300))


def svg_escape(text: str) -> str:
    return text.replace("&", "&amp;").replace("<", "&lt;").replace(">", "&gt;").replace('"', "&quot;")


def make_svg():
    # The SVG mirrors the publication figure as an editable vector source.
    content = f'''<svg xmlns="http://www.w3.org/2000/svg" width="{W}" height="{H}" viewBox="0 0 {W} {H}">
<rect width="{W}" height="{H}" fill="#FFFFFF"/>
<text x="90" y="92" font-family="Arial, Helvetica, sans-serif" font-size="44" font-weight="700" fill="#0F172A">{svg_escape("知识约束的路线类专题图智能制图方法")}</text>
<text x="90" y="138" font-family="Arial, Helvetica, sans-serif" font-size="24" fill="#64748B">{svg_escape("从自然语言路线叙事到可验证地图成果的语义表示、智能体协同与 GIS 执行机制")}</text>
<text x="90" y="1545" font-family="Arial, Helvetica, sans-serif" font-size="19" fill="#64748B">{svg_escape("注：正式论文排版可使用同名 PNG 预览，SVG 保留为可编辑矢量源文件。")}</text>
</svg>
'''
    FIG_SVG.write_text(content, encoding="utf-8")


SECTION3: list[tuple[str, str | None]] = [
    ("本章提出一种面向路线类专题图的知识约束智能制图方法。该方法针对自然语言路线叙事难以直接转化为 GIS 制图操作、通用大模型缺少路线制图语义约束、自动生成地图缺少质量反馈等问题，将路线文本解析、知识图谱组织、多智能体任务规划、QGIS 工具执行和多模态质量评价组织为统一的技术框架。与直接生成 PyQGIS 脚本或单次调用制图工具的方式不同，本文强调在工具执行前构造可检查的结构化制图状态，使系统能够在生成过程中显式追踪路线节点、历史事件、空间位置、表达方法、工具输入和质量反馈之间的关系。", None),
    ("图 3-1 知识约束的路线类专题图智能制图方法框架", "figure"),
    ("3.1 任务定义与总体框架", "Heading 2"),
    ("路线类专题图智能制图任务可定义为从自然语言路线叙事到可验证地图成果的转换过程。给定用户制图指令、历史文献或业务文本，系统需要识别其中的行动主体、时间线索、地点实体、路线节点、历史事件、节点顺序和成果表达要求，并结合已有空间数据与 QGIS 工程状态，生成包含路线线状符号、节点标注、事件说明、图例、比例尺、指北针和输出布局的专题地图。该任务的输入包括文本需求、空间数据、制图知识、模型配置和工具状态；输出包括结构化制图需求、路线制图知识图谱、工具调用序列、QGIS 图层与布局对象以及最终导出的地图成果。", None),
    ("本文方法由四类状态和五个模块共同构成。四类状态分别是语义状态、知识状态、执行状态和评价状态：语义状态记录从文本中抽取得到的路线节点、事件和表达要求；知识状态记录本体概念、图谱关系和质量约束；执行状态记录工具调用、图层变化、布局对象和导出结果；评价状态记录地图问题、证据来源和修订建议。五个模块分别为路线制图信息抽取、路线本体与知识图谱构建、任务型多智能体协同、QGIS 工具执行以及多模态质量评价。各模块不是顺序堆叠的功能列表，而是通过结构化状态传递形成闭环：文本解析产生语义状态，知识图谱约束任务规划，工具执行返回工程状态，质量评价再将问题定位反馈给路线表达或版式编排环节。", None),
    ("3.2 GIS 与 AI 智能体融合的系统架构", "Heading 2"),
    ("AutoCarto AI 采用用户交互层、Go 后端调度层、多智能体层和 QGIS 执行层组成的四层架构。用户交互层负责接收自然语言制图需求、路线文本和制图参数，并展示任务状态、工具日志和地图成果。Go 后端调度层负责维护会话上下文、任务队列、模型配置、工具网关和运行日志，将前端输入转化为可调度的结构化请求。多智能体层负责完成路线意图解析、数据可用性推理、路线关系组织、专题表达设计、版式编排和质量评价。QGIS 执行层以 QGIS/PyQGIS 为本地制图环境，将工具调用转化为图层加载、几何处理、符号化、标注配置、布局生成和成果导出等操作。", None),
    ("该架构的关键不是简单地把大模型接入 GIS，而是明确区分语言推理、任务调度和 GIS 执行的边界。大模型负责解析需求、组织路线语义和选择工具；Go 后端负责运行时状态维护、工具调用转发和错误记录；QGIS 负责可验证的空间数据处理和地图渲染。通过这种分层设计，系统可以避免模型直接生成不可控脚本，也可以避免 GIS 工具孤立执行而缺少语义约束。该设计在实验中可通过任务完成率、工具调用成功率、执行错误次数、人工干预次数和成果导出完整性等指标进行验证。", None),
    ("3.3 本体约束的路线制图信息抽取", "Heading 2"),
    ("路线类专题图的核心信息通常以叙事方式出现，例如某一行动主体在特定时间从起点出发，经过若干地点，并在途中发生战斗、转移、驻留或会师等事件。此类文本不能直接作为 GIS 绘图参数使用，原因在于地点实体、路线顺序、事件归属和表达意图往往隐含在句子结构中。为此，本文将信息抽取定义为本体约束下的语义结构化过程，即将原始文本解析为可供知识图谱、智能体规划和工具执行共享的制图需求。", None),
    ("抽取模式包括行动主体、时间信息、地点实体、路线节点、历史事件、路线顺序、节点重要性、事件说明和成果表达要求等字段。地点实体记录名称、别名、地理线索和地理编码需求；路线节点记录节点序号、节点类型、关联地点和标注等级；历史事件记录事件名称、发生时间、关联节点和说明文本；表达要求记录路线线型、节点符号、说明框、图例和导出格式等需求。模型输出需要满足预定义字段集合和类型约束，并经过字段完整性、节点顺序一致性和事件地点匹配检查。该模块的作用是把文本中的不确定叙事压缩为可检查的语义状态，从而减少后续工具调用中的字段漂移和参数缺失。", None),
    ("3.4 路线制图本体与知识图谱构建", "Heading 2"),
    ("在完成信息抽取后，系统构建面向路线类专题图的制图本体与知识图谱。该知识图谱不是通用地理知识库，而是围绕路线图自动制图任务组织的领域知识结构，用于显式描述制图任务、路线节点、路线段、历史事件、空间位置、行动主体、表达方法、地图要素、质量约束和输出成果之间的关系。其设计目标是把文本叙事中的隐含路线过程转化为可查询、可约束、可推理和可修订的制图知识。", None),
    ("典型关系包括：制图任务包含路线节点，路线节点按照顺序连接形成路线段，历史事件发生于路线节点，行动主体参与历史事件，路线段采用特定线状表达方法，重要事件需要说明框，地图成果包含标题、图例、比例尺和指北针，质量约束检查路线连贯性、节点完整性、事件匹配和版式可读性。通过这些关系，系统能够在智能体规划前明确“哪些节点需要连接、哪些事件需要标注、哪些符号需要进入图例、哪些质量问题需要检查”。知识图谱由此承担两类功能：一是为模型抽取和任务规划提供语义边界，二是为地图质量评价提供可核查的参照。", None),
    ("3.5 知识约束的任务型多智能体协同", "Heading 2"),
    ("路线类专题图制图链条长，涉及文本理解、数据检查、路线组织、符号设计、版式编排和质量评价等多类知识。若由单一模型一次性完成，容易出现职责混淆、执行顺序错误和质量问题难以定位等情况。本文依据路线类专题图制图过程设置中央协调智能体、路线意图解析智能体、数据推理智能体、路线表达智能体、版式编排智能体和质量评价智能体，使不同知识类型由相应角色处理。", None),
    ("中央协调智能体维护全局目标、任务状态和子任务顺序；路线意图解析智能体识别制图目标、行动主体、路线范围和成果形式；数据推理智能体检查路线节点、坐标字段、图层状态和 QGIS 工程环境；路线表达智能体依据图谱关系生成主路线、分支路线、多起点汇合路线和事件节点表达方案；版式编排智能体组织标题、图例、比例尺、指北针、节点标注和说明框；质量评价智能体根据知识图谱、QGIS 状态和地图图像输出问题定位与修订建议。各智能体之间传递的是结构化制图需求、图谱关系、工具执行结果和评价状态，而不是仅依赖自由文本对话。该机制使系统能够把复杂制图任务拆分为可追踪的局部决策，并为实验中的错误归因和消融分析提供基础。", None),
    ("3.6 知识驱动的 QGIS 制图执行机制", "Heading 2"),
    ("路线图制图执行的关键问题是如何将知识图谱中的节点、事件和表达约束转化为 QGIS 中可执行的地图操作。本文采用工具契约而非自由脚本生成的方式组织 QGIS 能力。每个工具均限定输入参数、输出结果、适用条件和失败反馈，并由 Go 后端统一调度。这样，智能体输出的不是不可检查的长脚本，而是一组可验证的工具调用序列。", None),
    ("路线类专题图工具链包括五类能力。第一类是数据接入与检查工具，用于加载路线节点、路线段、行政边界和底图数据，并检查坐标系统、字段名称和几何类型。第二类是路线关系构建工具，用于依据节点顺序、分支关系和多起点汇合关系生成路线段。第三类是路线表达工具，用于生成平滑路线、曲线箭头、主路线与分支路线符号，并处理路线叠压和层级关系。第四类是地图要素配置工具，用于生成节点标注、事件说明框、图例、标题、比例尺和指北针。第五类是成果导出工具，用于输出 PDF、PNG 或布局工程文件。工具执行过程中产生的图层状态、日志、布局对象和导出成果会被写入执行状态，供质量评价模块进一步检查。", None),
    ("3.7 多模态质量评价与反馈优化", "Heading 2"),
    ("路线类专题图的质量不仅取决于工具是否成功执行，还取决于地图是否准确表达路线过程、节点是否完整、事件注记是否对应、路线层次是否清晰以及版式是否便于阅读。为此，系统将用户原始需求、路线知识图谱、QGIS 图层与布局状态以及导出的地图图像共同作为质量评价输入，围绕路线连贯性、节点完整性、事件标注准确性、符号层次清晰度、图例一致性、标注可读性和版式完整性等维度开展检查。", None),
    ("评价过程采用规则检查与多模态模型判断相结合的方式。对于节点数量、图例项、地图要素完整性和路线顺序等可结构化检查内容，系统优先依据知识图谱和 QGIS 状态进行规则判断；对于视觉层次、标注遮挡、说明框位置和整体可读性等难以完全规则化的问题，系统将地图图像和评价提示输入多模态模型，生成问题描述和修改建议。评价结果进一步被转化为修订指令，反馈给路线表达智能体或版式编排智能体。例如，关键节点缺失时调用节点标注工具补充标注；主路线与分支路线层次不清时调整线宽、线型或颜色；说明框遮挡路线时重新计算说明框位置。该机制使系统从一次性地图生成扩展为“生成—评价—修订”的闭环制图流程。", None),
]


def insert_paragraph_after(paragraph: Paragraph, text: str = "", style: str | None = None) -> Paragraph:
    new_p = OxmlElement("w:p")
    paragraph._p.addnext(new_p)
    new_para = Paragraph(new_p, paragraph._parent)
    if style:
        new_para.style = style
    if text:
        new_para.add_run(text)
    return new_para


def delete_paragraph(paragraph: Paragraph):
    p = paragraph._element
    p.getparent().remove(p)
    paragraph._p = paragraph._element = None


def add_figure_after(paragraph: Paragraph, image_path: Path, caption: str) -> Paragraph:
    img_p = insert_paragraph_after(paragraph)
    img_p.alignment = WD_ALIGN_PARAGRAPH.CENTER
    img_p.paragraph_format.space_before = Pt(6)
    img_p.paragraph_format.space_after = Pt(2)
    img_p.add_run().add_picture(str(image_path), width=Cm(15.8))
    cap_p = insert_paragraph_after(img_p, caption)
    cap_p.alignment = WD_ALIGN_PARAGRAPH.CENTER
    cap_p.paragraph_format.space_after = Pt(8)
    for run in cap_p.runs:
        run.font.size = Pt(9)
    return cap_p


def locate_section(doc: Document, heading: str = "3 系统架构与方法"):
    start = end = None
    for i, para in enumerate(doc.paragraphs):
        text = para.text.strip()
        if text == heading:
            start = i
        elif start is not None and text.startswith("4 "):
            end = i
            break
    if start is None or end is None:
        raise RuntimeError("Cannot locate section 3 range")
    return start, end


def rewrite_doc():
    make_png()
    make_svg()
    doc = Document(SRC)
    start, end = locate_section(doc)
    for idx in range(end - 1, start, -1):
        delete_paragraph(doc.paragraphs[idx])

    anchor = doc.paragraphs[start]
    for text, kind in SECTION3:
        if kind == "figure":
            anchor = add_figure_after(anchor, FIG_PNG, text)
        else:
            anchor = insert_paragraph_after(anchor, text, style=kind)

    if OUT.exists():
        OUT.unlink()
    doc.save(OUT)
    print(OUT)
    print(FIG_PNG)
    print(FIG_SVG)


if __name__ == "__main__":
    rewrite_doc()
