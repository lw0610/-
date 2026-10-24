from copy import deepcopy
from datetime import datetime, timezone
from pathlib import Path
from zipfile import ZipFile, ZIP_DEFLATED
import shutil
import tempfile

from lxml import etree


SRC = Path("/Users/zw/Downloads/AutoCarto_专题图制图智能体论文-修订模式.docx")
OUT = Path("/Users/zw/研究生/QGIS-agent/Eino/AutoCarto_专题图制图智能体论文_批注建议版.docx")

NS = {"w": "http://schemas.openxmlformats.org/wordprocessingml/2006/main"}
W = NS["w"]


COMMENTS = [
    {
        "starts": "知识驱动的专题图智能制图方法",
        "comment": "标题仍然偏泛，像一个方向名称而不是论文贡献。正文实际方法和实验主要落在路线类专题图、知识图谱约束和多智能体协同上。建议题目突出方法创新和任务边界，例如“知识图谱约束的路线类专题图多智能体制图方法”或“面向业务文本的专题图知识约束制图方法”。",
    },
    {
        "starts": "摘要：专题图制图通常需要",
        "comment": "摘要结构比上一版清楚，但仍偏长，一句话承载过多信息。建议按“背景问题、研究缺口、本文方法、实验结果、意义边界”压缩为5到6句。另一个关键问题是末句已经写“优于直接脚本生成方法”，如果实验数据目前还没补齐，应改为带红色占位的量化结果，或先写“初步验证了可行性”，避免证据不足。",
    },
    {
        "starts": "摘要：专题图制图通常需要",
        "comment": "摘要和关键词现在被放在同一个段落里，正式论文排版应分开。建议摘要单独成段，关键词另起一段，并统一关键词为真正的核心概念，如“专题图制图、制图语义解析、知识图谱、多智能体、质量反馈、QGIS”。",
    },
    {
        "starts": "专题图是地图表达的重要形式",
        "comment": "引言开头的背景交代是合理的，但仍有一点教材化。可以更快切到研究问题，即“专题图制图为什么难自动化”。建议第一段保留背景和意义，第二段立即进入文本需求、制图知识和人工流程瓶颈。",
    },
    {
        "starts": "在实际应用中，专题图制图需求通常",
        "comment": "这一段的问题意识较强，是引言里比较有效的部分。建议进一步把“文本材料到地图成果”的困难拆成三个明确瓶颈：语义抽取、表达规则选择、质量反馈。这样后面的贡献能一一对应。",
    },
    {
        "starts": "近年来，大语言模型在自然语言理解",
        "comment": "这一段把已有研究概括得比较泛，缺少具体文献落点。建议至少在通用GIS智能体、地图设计智能体、特定制图任务三类后分别给出代表文献编号，否则读者会觉得这是概括性判断而不是文献综述。",
    },
    {
        "starts": "然而，现有研究在面向专题图制图任务时仍存在不足",
        "comment": "这里的三个不足是成立的，但与相关研究结尾处的不足高度重复。建议引言中保留更凝练的版本，相关研究中再展开论证，避免同一组问题在多个位置重复出现。",
    },
    {
        "starts": "针对上述问题，本文提出一种知识约束",
        "comment": "这里需要解决全文范围不一致问题。摘要和引言说的是“专题图智能制图”，但方法和实验部分大量转向“路线类专题图”和“AutoCarto AI”。建议明确本文是“以路线类专题图为典型对象验证的专题图智能制图方法”，或者直接把题目和全文范围收窄到路线类专题图。",
    },
    {
        "starts": "本文的主要贡献包括以下三个方面",
        "comment": "贡献写法比原稿清楚，但仍偏“做了什么”，还不够像“创新点”。建议每条贡献改成“针对什么不足，提出什么方法，带来什么可验证改进”。例如第一条不仅说语义解析与知识组织，还要强调本体/知识图谱如何约束后续工具调用。",
    },
    {
        "starts": "针对上述问题，本文以路线类专题图作为典型实验对象",
        "comment": "引言最后一段写实验是可以的，但现在结果表述偏满，尤其“实验结果表明”“具有较好的应用效果”需要后文数据支撑。若实验数据暂未完善，建议改为“本文以长征路线图为例开展原型验证，并从……方面设计评价”，等数据补齐后再写比较结论。",
    },
    {
        "starts": "近年来，大语言模型在自然语言理解、任务规划和工具调用方面",
        "comment": "相关研究这一段需要补充规范引用。Autonomous GIS、ChatGeoAI、Smart-QGIS等系统都被点名，但段内没有对应参考文献编号，正式论文中应在系统名称后或句末标注引用。",
    },
    {
        "starts": "专题图制图是一项高度依赖专业知识",
        "comment": "这一段讨论制图知识建模，但缺少与本文“知识图谱/本体”的差异定位。建议补一句：现有规则或模板方法解决了局部表达问题，但难以处理自然语言需求中的任务语义和跨环节状态传递，由此引出本文知识组织的必要性。",
    },
    {
        "starts": "现有地图智能设计研究也开始引入多智能体机制",
        "comment": "这一段适合作为“多智能体制图研究”小节，但目前相关研究没有小标题，段落之间像连续铺陈。建议相关研究分为三类小节：大语言模型驱动GIS工具调用、制图知识组织与规则约束、多智能体制图与质量反馈。这样更像论文综述。",
    },
    {
        "starts": "总体来看，现有研究已经验证了",
        "comment": "这个总结段逻辑完整，但和引言“不足”段重复度高。建议在相关研究结尾更强调“本文与已有工作的具体差异”，例如从通用工具调用转向专题图语义约束，从局部地图设计转向完整制图流程，从结果生成转向质量反馈闭环。",
    },
    {
        "starts": "本文面向专题图制图中自然语言需求难以直接转化",
        "comment": "结论第一段仍像摘要复述，句子较长。建议压缩为三句话：本文提出什么方法；该方法由哪些关键模块构成；相比直接脚本或工具调用，其边界和优势在哪里。",
    },
    {
        "starts": "以长征路线图为典型任务的实验表明",
        "comment": "这里的比较判断需要实验数据支撑。若表格数据尚未补齐，不建议直接写“表现出更好的适应性”。可以改为“初步结果显示……”，并用红色占位补充任务完成率、工具调用错误次数、人工干预次数和质量评分变化。",
    },
    {
        "starts": "后续研究将继续围绕方法能力",
        "comment": "展望方向是合理的，但仍偏泛。建议把展望改成与本文局限一一对应：一是补充多类型专题图实验，二是完善制图知识图谱和技能库，三是提升质量评价到修订指令的稳定性，四是扩展地名解析与数据匹配能力。",
    },
]


def para_text(p):
    return "".join(p.xpath(".//w:t/text()", namespaces=NS)).strip()


def next_comment_id(comments_root):
    ids = []
    for c in comments_root.xpath("./w:comment", namespaces=NS):
        v = c.get(f"{{{W}}}id")
        if v is not None:
            ids.append(int(v))
    return max(ids, default=-1) + 1


def add_comment_to_comments(comments_root, cid, text):
    c = etree.SubElement(comments_root, f"{{{W}}}comment")
    c.set(f"{{{W}}}id", str(cid))
    c.set(f"{{{W}}}author", "Codex")
    c.set(f"{{{W}}}date", datetime.now(timezone.utc).isoformat())

    p = etree.SubElement(c, f"{{{W}}}p")
    r = etree.SubElement(p, f"{{{W}}}r")
    t = etree.SubElement(r, f"{{{W}}}t")
    t.text = text


def anchor_comment(paragraph, cid):
    start = etree.Element(qn("w:commentRangeStart"))
    start.set(qn("w:id"), str(cid))
    end = etree.Element(qn("w:commentRangeEnd"))
    end.set(qn("w:id"), str(cid))

    ref_run = etree.Element(qn("w:r"))
    ref_pr = etree.Element(qn("w:rPr"))
    ref_run.append(ref_pr)
    cref = etree.Element(qn("w:commentReference"))
    cref.set(qn("w:id"), str(cid))
    ref_run.append(cref)

    children = list(paragraph)
    insert_pos = 0
    while insert_pos < len(children) and not children[insert_pos].tag.endswith("}r"):
        insert_pos += 1
    paragraph.insert(insert_pos, start)
    paragraph.append(end)
    paragraph.append(ref_run)


def qn(tag):
    prefix, local = tag.split(":")
    if prefix != "w":
        raise ValueError(tag)
    return f"{{{W}}}{local}"


def main():
    if not SRC.exists():
        raise FileNotFoundError(SRC)

    tmp = Path(tempfile.mkdtemp(prefix="autocarto_review_"))
    try:
        with ZipFile(SRC, "r") as zin:
            zin.extractall(tmp)

        doc_path = tmp / "word" / "document.xml"
        comments_path = tmp / "word" / "comments.xml"

        parser = etree.XMLParser(remove_blank_text=False)
        doc_root = etree.parse(str(doc_path), parser).getroot()
        comments_root = etree.parse(str(comments_path), parser).getroot()

        paragraphs = doc_root.xpath("//w:body/w:p", namespaces=NS)
        used = set()
        cid = next_comment_id(comments_root)
        added = 0

        for spec in COMMENTS:
            target = None
            for p in paragraphs:
                text = para_text(p)
                if text.startswith(spec["starts"]) and id(p) not in used:
                    target = p
                    break
            if target is None:
                # Fallback to contains, still avoid duplicate anchoring.
                for p in paragraphs:
                    text = para_text(p)
                    if spec["starts"][:18] in text and id(p) not in used:
                        target = p
                        break
            if target is None:
                continue
            add_comment_to_comments(comments_root, cid, spec["comment"])
            anchor_comment(target, cid)
            used.add(id(target))
            cid += 1
            added += 1

        etree.ElementTree(doc_root).write(str(doc_path), encoding="UTF-8", xml_declaration=True, standalone="yes")
        etree.ElementTree(comments_root).write(str(comments_path), encoding="UTF-8", xml_declaration=True, standalone="yes")

        if OUT.exists():
            OUT.unlink()
        with ZipFile(OUT, "w", ZIP_DEFLATED) as zout:
            for path in tmp.rglob("*"):
                if path.is_file():
                    zout.write(path, path.relative_to(tmp).as_posix())
        print(f"wrote={OUT}")
        print(f"comments_added={added}")
    finally:
        shutil.rmtree(tmp, ignore_errors=True)


if __name__ == "__main__":
    main()
