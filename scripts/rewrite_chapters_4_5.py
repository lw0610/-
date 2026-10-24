from pathlib import Path

from docx import Document
from docx.enum.text import WD_ALIGN_PARAGRAPH
from docx.oxml import OxmlElement
from docx.oxml.ns import qn
from docx.shared import RGBColor, Pt
from docx.table import Table
from docx.text.paragraph import Paragraph


ROOT = Path("/Users/zw/研究生/QGIS-agent/Eino")
SRC = ROOT / "AutoCarto_专题图制图智能体论文_系统架构与方法重写版.docx"
OUT = ROOT / "AutoCarto_专题图制图智能体论文_第4-5章优化版.docx"


CH45 = [
    ("4 应用与实验", "Heading 1"),
    ("为检验 AutoCarto AI 在路线类专题图制图任务中的可行性，本文以路线图生成为典型应用场景，围绕“自然语言路线叙事—结构化制图知识—QGIS 自动制图—质量评价反馈”的完整链路开展实验。实验不以覆盖所有 GIS 制图任务为目标，而是重点考察系统在路线节点抽取、路线关系组织、工具调用执行、地图要素配置和反馈修订等环节中的表现。与只展示最终地图成果不同，本章同时记录中间结构化结果、知识组织结果和执行日志，以说明系统能否将文本中的路线语义稳定传递到地图生成过程。", None),
    ("4.1 实验设计与评价指标", "Heading 2"),
    ("实验任务以行军路线图制作为代表。该类任务通常包含行动主体、行军时间、途经地点、路线顺序、关键事件和制图表达要求等信息，能够较集中地体现路线类专题图中“文本语义—空间关系—地图表达”之间的转换问题。实验输入包括路线叙事文本、底图与行政边界数据、节点坐标或可定位地点信息、QGIS 工程环境以及 AutoCarto AI 的模型与工具配置。实验输出包括结构化文本解析结果、路线制图知识图谱、智能体工具调用日志、自动生成地图以及质量评价与修订结果。", None),
    ("为避免仅以个例展示系统功能，实验任务按照路线结构和表达复杂度划分为三类：简单任务对应单一路线和少量关键节点；中等任务包含主路线、分支路线或多个事件说明；复杂任务包含多起点汇合、多段路线关系、事件注记和较完整的版式表达。评价指标分为过程指标和成果指标两类。过程指标包括任务完成率、工具调用成功率、工具调用错误次数、人工干预次数和制图耗时；成果指标包括节点完整性、路线连贯性、事件标注准确性、图例一致性、版式完整性和综合质量评分。表 4-1 给出了实验评价指标及其含义。", None),
    ("表 4-1 路线类专题图自动制图实验评价指标", "table:metrics"),
    ("4.2 路线文本解析与知识组织结果", "Heading 2"),
    ("路线类专题图的制图信息通常并不以标准参数形式给出，而是隐含在历史文献、业务材料或用户自然语言描述中。实验首先对路线文本进行结构化解析，将原始叙事中的行动主体、时间信息、地点实体、路线节点、历史事件、节点顺序和成果表达要求抽取为统一字段。示例输入文本如下：", None),
    ("“1935 年 1 月，中央红军从遵义出发，经桐梓、娄山关等地向黔北转移。2 月 25 日，红军攻占娄山关，随后二渡赤水，回师黔北。制图时需要突出主行军路线，标注遵义、桐梓、娄山关和赤水等关键节点，并对娄山关战斗进行说明。”", "quote"),
    ("在本体约束的抽取模式下，系统需要识别“中央红军”为行动主体，“遵义、桐梓、娄山关、赤水”为路线节点，“娄山关战斗”为历史事件，并形成“遵义—桐梓—娄山关—赤水”的节点顺序关系。同时，文本中的“突出主行军路线”“标注关键节点”“对娄山关战斗进行说明”等描述被转换为主路线强调、节点标注和事件说明框配置等制图表达要求。该步骤的重点不是生成文本摘要，而是形成可检查、可传递的制图语义状态。", None),
    ("图 4-1 路线文本输入与结构化解析结果", "placeholder"),
    ("在完成信息抽取后，系统进一步将时间、地点、事件、路线顺序和表达要求组织为路线制图知识图谱。知识图谱显式表达路线任务、路线节点、历史事件、路线关系、表达方法和地图要素之间的关系。例如，“娄山关战斗—发生于—娄山关”表示事件与空间节点的对应关系，“主路线—采用—突出线状符号”表示路线类型与表达方法之间的映射关系，“地图成果—包含—标题、图例、比例尺、指北针”表示地图成果应具备的版式要素。通过知识图谱组织，系统将文本中的隐含路线关系显式化，为后续智能体规划和 QGIS 工具调用提供约束。", None),
    ("图 4-2 路线制图知识图谱组织结果", "placeholder"),
    ("4.3 路线图自动生成结果", "Heading 2"),
    ("在路线知识图谱构建完成后，系统进入自动制图阶段。中央协调智能体依据制图需求和图谱关系组织任务顺序，并调用相应 QGIS 工具完成数据检查、路线段生成、符号配置、节点标注、事件说明框生成、图例配置、版式组织和成果导出等操作。路线意图解析智能体负责确认制图目标和成果形式，数据推理智能体检查节点坐标、底图图层和 QGIS 工程状态，路线表达智能体依据节点顺序和路线关系生成主路线及相关路线段，版式编排智能体负责地图要素组织，最终由 QGIS 完成地图渲染与布局导出。", None),
    ("图 4-3 展示了智能体协同制图执行日志。执行日志用于记录不同智能体的调用顺序、工具输入参数、工具返回状态和异常反馈。与直接生成完整 PyQGIS 脚本相比，基于工具契约的执行方式能够将长流程制图任务拆分为可检查的中间步骤，使错误更容易定位。例如，当节点坐标字段缺失、图层路径无效或布局导出失败时，系统能够从工具反馈中获得明确错误信息，并将其返回给调度层或相关智能体。", None),
    ("图 4-3 智能体协同制图执行日志", "placeholder"),
    ("图 4-4 展示了系统自动生成的路线类专题图成果。该成果应包含主路线、关键节点、事件说明框、图例、比例尺、指北针和标题等地图要素，并能够表达输入文本中的主要路线过程与关键事件。由于当前系统仍处于完善阶段，图 4-4 的最终制图效果和对应实验数据需在后续系统稳定后补充。", None),
    ("图 4-4 自动生成的路线类专题图成果", "placeholder"),
    ("4.4 质量评价与反馈修订", "Heading 2"),
    ("路线类专题图的生成质量不仅取决于地图是否能够成功导出，还取决于路线关系是否连贯、关键节点是否完整、事件说明是否准确、图例是否一致以及整体版式是否清晰。为检验系统的反馈修订能力，实验将自动生成的初始地图、原始制图需求、路线知识图谱以及 QGIS 图层与布局状态共同输入质量评价模块，对地图成果进行检查。", None),
    ("质量评价围绕路线连贯性、节点完整性、事件标注准确性、符号层次、图例一致性、标注可读性和版式完整性展开。对于节点数量、图例项、地图要素完整性和路线顺序等可结构化检查内容，系统依据知识图谱和 QGIS 状态进行规则判断；对于标注遮挡、视觉层次和整体可读性等难以完全规则化的问题，系统结合地图图像进行多模态评价。评价结果随后被转换为修订指令，反馈给路线表达智能体或版式编排智能体，用于调整线宽、线型、节点标注、说明框位置和图例内容。", None),
    ("图 4-5 路线类专题图质量评价结果", "placeholder"),
    ("图 4-6 反馈修订后的路线类专题图成果", "placeholder"),
    ("总体来看，第 4 章实验部分主要用于验证系统流程的可执行性和闭环能力。当前版本重点保留实验设计、评价指标和结果呈现位置，具体任务数量、对比实验数据、质量评分和修订前后量化结果将在系统进一步稳定后补充。", "rednote"),
    ("5 讨论", "Heading 1"),
    ("从实验设计可以看出，AutoCarto AI 的核心价值并不在于替代所有 GIS 制图操作，而在于为路线类专题图建立从自然语言到结构化制图过程的中间表示。路线图制作的难点不是单纯绘制线状要素，而是需要同时处理路线节点顺序、历史事件归属、空间位置匹配、专题符号表达和版式组织。若直接由通用大语言模型生成 PyQGIS 脚本，模型需要在一次输出中同时完成需求理解、字段判断、代码生成和版式控制，容易受到图层状态、字段名称、路径配置和参数缺失等因素影响。本文方法通过文本解析和知识图谱组织先形成明确的路线制图知识，再由多智能体选择工具并执行局部操作，从而提高长流程制图任务的可控性。", None),
    ("与专业人员人工制图相比，AutoCarto AI 的潜在优势主要体现在文本信息整理、重复性工具操作和质量反馈记录等环节。人工制图能够保持较高的专业判断和成果可控性，但需要手动阅读文本、整理节点、绘制路线、配置符号和组织版式，过程耗时且难以形成可复用的中间状态。AutoCarto AI 将路线节点、事件说明、表达要求和评价结果记录为结构化状态，使同类路线图任务可以复用抽取模式、知识关系和工具链。表 5-1 用于后续补充不同方法在任务完成率、制图耗时、人工干预次数、工具调用错误次数和地图质量评分等方面的对比结果。", None),
    ("表 5-1 不同方法在路线类专题图任务中的制图效果对比", "table:performance"),
    ("与现有智能制图系统相比，AutoCarto AI 的定位也更偏向特定专题图类型的知识约束制图。Smart-QGIS 等系统更关注自然语言到通用 GIS 工具的协议化调用，MapGPT 侧重于大语言模型与制图工具集成的地图生成，CartoAgent 强调多模态大模型和多智能体机制在地图风格迁移与评价中的应用。相比之下，AutoCarto AI 将研究对象限定为路线类专题图，重点处理非结构化路线文本中的时间、地点、事件和路线关系，并将其转化为路线段、节点标注、事件说明框和版式要素等可执行制图内容。表 5-2 对不同系统的能力侧重点进行了概括。", None),
    ("表 5-2 AutoCarto AI 与代表性智能制图系统能力对比", "table:systems"),
    ("需要指出的是，当前研究仍存在三方面限制。第一，实验对象主要集中在路线类专题图，尚未覆盖统计专题图、分区设色图、流向图和多变量综合图等其他地图类型，因此方法的通用性仍需通过更多任务验证。第二，路线文本解析和事件地点匹配仍依赖模型对自然语言的理解，当文本存在地点别名、历史地名、模糊方位或缺失坐标时，系统仍需要人工校正或外部地名知识库支持。第三，地图质量评价中的视觉判断仍具有一定主观性，尤其是标注遮挡、版式均衡和符号层次等问题，需要进一步结合人工评价、规则约束和多模态模型输出建立更稳定的评价标准。", None),
    ("因此，本文方法更适合作为路线类专题图智能制图的原型框架，而不是已经完全成熟的通用制图系统。后续工作应从三个方向继续推进：一是补充不同复杂度路线任务的定量实验，完善任务完成率、工具调用成功率、质量评分和修订效果等结果；二是引入更丰富的地名知识、历史事件知识和制图规则，提升系统对复杂文本和复杂路线关系的处理能力；三是完善地图质量评价标准，将规则检查、多模态模型判断和人工评价结果结合起来，形成可复现的专题图质量评价协议。", None),
]


def set_cell_text(cell, text, red=False):
    cell.text = ""
    p = cell.paragraphs[0]
    p.alignment = WD_ALIGN_PARAGRAPH.CENTER
    run = p.add_run(text)
    run.font.size = Pt(9)
    if red:
        run.font.color.rgb = RGBColor(192, 0, 0)


def style_table(table: Table):
    table.style = "Table Grid"
    for row in table.rows:
        for cell in row.cells:
            for p in cell.paragraphs:
                for run in p.runs:
                    run.font.size = Pt(9)


def insert_paragraph_after(paragraph: Paragraph, text: str = "", style: str | None = None) -> Paragraph:
    new_p = OxmlElement("w:p")
    paragraph._p.addnext(new_p)
    new_para = Paragraph(new_p, paragraph._parent)
    if style:
        new_para.style = style
    if text:
        run = new_para.add_run(text)
        if style == "rednote":
            run.font.color.rgb = RGBColor(192, 0, 0)
    return new_para


def insert_table_after(doc: Document, anchor: Paragraph, kind: str) -> Paragraph:
    if kind == "table:metrics":
        rows = [
            ["指标类别", "评价指标", "含义"],
            ["过程指标", "任务完成率", "成功生成目标地图成果的任务比例"],
            ["过程指标", "工具调用成功率", "工具调用返回成功状态的比例"],
            ["过程指标", "人工干预次数", "任务执行过程中需要人工修正的次数"],
            ["过程指标", "制图耗时", "从任务输入到成果导出的总时间"],
            ["成果指标", "节点完整性", "关键路线节点是否完整表达与标注"],
            ["成果指标", "路线连贯性", "路线是否按知识图谱顺序正确连接"],
            ["成果指标", "事件标注准确性", "事件说明是否与对应空间节点匹配"],
            ["成果指标", "版式完整性", "标题、图例、比例尺、指北针和出图范围是否完整"],
        ]
    elif kind == "table:performance":
        rows = [
            ["方法", "任务完成率/%", "平均人工干预次数/次", "平均工具调用错误次数/次", "平均耗时/min", "平均质量评分"],
            ["专业人员人工制图", "待补充", "待补充", "待补充", "待补充", "待补充"],
            ["通用大语言模型生成 PyQGIS 脚本", "待补充", "待补充", "待补充", "待补充", "待补充"],
            ["AutoCarto AI", "待补充", "待补充", "待补充", "待补充", "待补充"],
        ]
    elif kind == "table:systems":
        rows = [
            ["系统", "核心定位", "非结构化文本处理", "路线语义约束", "质量反馈闭环", "本地部署支持"],
            ["Smart-QGIS[3]", "通用空间数据处理与制图智能体", "部分支持", "不支持", "不支持", "支持"],
            ["MapGPT[4]", "大语言模型与制图工具集成的地图生成框架", "部分支持", "不支持", "不支持", "部分支持"],
            ["CartoAgent[8]", "地图风格迁移与评价的多智能体制图框架", "部分支持", "不支持", "部分支持", "部分支持"],
            ["AutoCarto AI", "面向路线类专题图的闭环制图智能体", "支持", "支持", "支持", "支持"],
        ]
    else:
        raise ValueError(kind)

    table = doc.add_table(rows=len(rows), cols=len(rows[0]))
    style_table(table)
    for i, row in enumerate(rows):
        for j, value in enumerate(row):
            red = value == "待补充"
            set_cell_text(table.cell(i, j), value, red=red)
    anchor._p.addnext(table._tbl)
    spacer = insert_paragraph_after(Paragraph(table._tbl, anchor._parent), "")
    return spacer


def delete_block(el):
    parent = el.getparent()
    if parent is not None:
        parent.remove(el)


def locate_heading_elements(doc: Document):
    starts = {}
    for p in doc.paragraphs:
        text = p.text.strip()
        if text in {"4 应用与实验", "5 讨论", "6 结论与展望"}:
            starts[text] = p._p
    if "4 应用与实验" not in starts or "6 结论与展望" not in starts:
        raise RuntimeError("Cannot locate chapter 4 and chapter 6 boundaries")
    return starts["4 应用与实验"], starts["6 结论与展望"]


def main():
    doc = Document(SRC)
    start_el, end_el = locate_heading_elements(doc)

    body = doc.element.body
    children = list(body)
    start_idx = children.index(start_el)
    end_idx = children.index(end_el)

    # Remove chapter 4 and 5 blocks, keep chapter 6 and references unchanged.
    for el in children[end_idx - 1 : start_idx - 1 : -1]:
        delete_block(el)

    # Insert a temporary anchor before chapter 6.
    anchor_p = OxmlElement("w:p")
    end_el.addprevious(anchor_p)
    anchor = Paragraph(anchor_p, doc._body)

    current = anchor
    for text, style in CH45:
        if style and style.startswith("table:"):
            current = insert_paragraph_after(current, text)
            current.alignment = WD_ALIGN_PARAGRAPH.CENTER
            current = insert_table_after(doc, current, style)
        elif style == "placeholder":
            current = insert_paragraph_after(current, text)
            run = current.add_run("（图件待补充）")
            run.font.color.rgb = RGBColor(192, 0, 0)
            current.alignment = WD_ALIGN_PARAGRAPH.CENTER
        elif style == "quote":
            current = insert_paragraph_after(current, text)
            for run in current.runs:
                run.font.italic = True
        elif style == "rednote":
            current = insert_paragraph_after(current, text)
            for run in current.runs:
                run.font.color.rgb = RGBColor(192, 0, 0)
        else:
            current = insert_paragraph_after(current, text, style=style)

    delete_block(anchor._p)

    if OUT.exists():
        OUT.unlink()
    doc.save(OUT)
    print(OUT)


if __name__ == "__main__":
    main()
