from __future__ import annotations

import csv
import re
from html import escape
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
TOOLS_DIR = ROOT / "autocarto" / "tools" / "system"
OUT_DIR = ROOT / "docs" / "function_map"
OUT_SVG = OUT_DIR / "autocarto_function_map.svg"
OUT_CSV = OUT_DIR / "autocarto_tool_catalog.csv"

W = 4200
FONT_STACK = (
    "'PingFang SC','Microsoft YaHei','Noto Sans CJK SC',"
    "'Hiragino Sans GB',Arial,sans-serif"
)


# 一级功能只做分类；二级功能由下列目录中的真实 Tool YAML 生成。
TOOL_GROUPS = [
    {
        "title": "工程与地图画布",
        "domains": ["canvas-navigation", "project-state"],
    },
    {
        "title": "数据接入与文件管理",
        "domains": ["data-loading", "file-operations"],
    },
    {
        "title": "坐标参考与格式转换",
        "domains": ["crs-projection", "data-conversion"],
    },
    {
        "title": "地理编码与 GPS",
        "domains": ["geocoding", "gps-data"],
    },
    {
        "title": "矢量要素创建",
        "domains": ["vector-creation"],
    },
    {
        "title": "矢量属性与要素筛选",
        "domains": ["vector-fields", "vector-selection"],
    },
    {
        "title": "矢量几何处理",
        "domains": ["vector-geometry"],
        "include": ["merge_vector_layers"],
    },
    {
        "title": "栅格地形与插值",
        "domains": ["raster-analysis"],
    },
    {
        "title": "空间、网络与可视性分析",
        "domains": ["spatial-analysis", "network-analysis", "visibility-analysis"],
    },
    {
        "title": "GDAL 与 GRASS 处理",
        "domains": ["gdal-processing", "grass-analysis"],
    },
    {
        "title": "点云、网孔与统计图表",
        "domains": ["point-cloud", "misc-tools"],
        "exclude": ["merge_vector_layers"],
    },
    {
        "title": "符号化与专题制图",
        "domains": ["cartography"],
        "exclude": ["cartography_build_and_draw_from_tables", "export_layout_pdf", "import_layout_template"],
    },
    {
        "title": "打印布局与成果输出",
        "domains": ["print-layout"],
        "include": ["export_layout_pdf", "import_layout_template"],
    },
    {
        "title": "行军路线数据、制图与质量复检",
        "domains": ["marching-route-knowledge", "quality-feedback"],
        "include": ["cartography_build_and_draw_from_tables"],
    },
    {
        "title": "脚本执行与运行控制",
        "domains": ["build-capability-awareness", "pyqgis-scripting", "runtime-control"],
    },
    {
        "title": "运行方案与功能包配置",
        "domains": ["creator-tools"],
    },
]


# 公司评审版以“一级功能—二级功能”展示行军路线专题图流程。
# 二级功能使用业务可读、同时能对应真实工具或执行环节的名称。
WORKFLOW_PHASES = [
    {
        "title": "制图知识抽取",
        "owner": "知识抽取智能体",
        "functions": [
            "知识抽取规则调用",
            "主体、地点与事件识别",
            "节点顺序与路线关系组织",
            "符号与标注要求解析",
            "任务知识图谱构建",
            "知识图谱转六张制图数据表",
        ],
        "responsibility": "从制图文本中抽取主体、地点、事件和路线关系，构建任务知识图谱并转换为制图数据表",
    },
    {
        "title": "底图数据准备",
        "owner": "数据智能体",
        "functions": [
            "底图图层加载",
            "底图数据检查",
        ],
        "responsibility": "加载制图所需的底图图层，并检查底图数据是否可用",
    },
    {
        "title": "行军路线绘制",
        "owner": "制图智能体",
        "functions": [
            "底图符号化",
            "路线数据读取",
            "曲线路线绘制",
            "路线与事件符号化",
            "图文标注生成",
        ],
        "responsibility": "完成底图符号化、行军路线绘制、路线与事件符号化以及图文标注生成",
    },
    {
        "title": "专题版面编排",
        "owner": "制图智能体",
        "functions": [
            "版面创建与模板应用",
            "地图框配置",
            "标题、图例等要素添加",
            "图文标注与版面要素布局",
        ],
        "responsibility": "创建或应用版面模板，配置地图框和必要的版面要素，完成整体布局",
    },
    {
        "title": "质量反馈与版面导出",
        "owner": "质量反馈智能体（检查）·制图智能体（修改导出）",
        "owner_lines": ["检查：质量反馈智能体", "修改与导出：制图智能体"],
        "functions": [
            "遮挡越界检查",
            "版面布局检查",
            "文字、配色与可读性检查",
            "问题元素定位与修改建议",
            "修改结果复检",
            "版面导出",
        ],
        "responsibility": "检查初版版面的遮挡、布局和可读性问题，定位问题并提出修改建议，复检后导出版面",
    },
]


# 只对过长、带内部术语或不适合直接上图的描述做等义缩写。
LABEL_OVERRIDES = {
    "query_build_capabilities": "查询当前方案可用功能",
    "assign_skill_packages": "为运行方案分配功能包",
    "create_agent_config": "创建运行方案",
    "list_agent_configs": "列出运行方案",
    "update_agent_config": "更新运行方案",
    "cartography_build_and_draw_from_tables": "按六表数据绘制路线、点符号和图文注记",
    "marching_route_compile_dataset": "校验路线知识数据并生成六张 CSV、符号资产和溯源文件",
    "marching_route_get_extraction_contract": "生成行军路线知识抽取规则和数据结构",
    "layout_get_items_properties": "批量读取布局元素的位置、尺寸、类型和层级",
    "layout_move_callout": "整体移动图文标注并重新计算牵引线",
    "layout_resize_item": "调整布局元素大小",
    "load_review_images": "读取初版和终稿质量检查图片",
    "prepare_quality_review": "保存初版快照并准备质量检查资料",
}


# YAML 已声明，但当前 handler 模块没有导出对应函数，不计入可执行二级功能。
BROKEN_BINDINGS = {
    "cartography_symbolize_points_by_mapping",
    "layout_get_item_corners",
    "layout_set_map_extent",
    "layout_set_map_scale",
}


def _unquote(value: str) -> str:
    value = value.strip()
    if len(value) >= 2 and value[0] == value[-1] and value[0] in {"'", '"'}:
        return value[1:-1]
    return value


def read_tool_yaml(path: Path) -> dict[str, str]:
    """Read the small metadata subset shared by the project's Tool YAML files."""
    lines = path.read_text(encoding="utf-8").splitlines()
    section = ""
    values: dict[str, str] = {}
    index = 0
    while index < len(lines):
        line = lines[index]
        if line and not line.startswith(" ") and line.endswith(":"):
            section = line[:-1]
            index += 1
            continue

        if section == "metadata" and line.startswith("  "):
            stripped = line.strip()
            if ":" in stripped:
                key, value = stripped.split(":", 1)
                if key in {"name", "runtime", "domain"}:
                    values[key] = _unquote(value)

        if section == "spec" and line.startswith("  description:"):
            raw = line.split(":", 1)[1].strip()
            if raw in {">", ">-", "|", "|-"}:
                folded: list[str] = []
                probe = index + 1
                while probe < len(lines):
                    candidate = lines[probe]
                    if candidate.strip() and len(candidate) - len(candidate.lstrip()) <= 2:
                        break
                    if candidate.strip():
                        folded.append(candidate.strip())
                    probe += 1
                values["description"] = " ".join(folded)
                index = probe
                continue
            values["description"] = _unquote(raw)
        index += 1

    missing = {"name", "runtime", "domain", "description"} - values.keys()
    if missing:
        raise ValueError(f"{path}: missing {sorted(missing)}")
    values["path"] = str(path.relative_to(ROOT))
    return values


def concise_label(tool: dict[str, str]) -> str:
    tool_id = tool["name"]
    if tool_id in LABEL_OVERRIDES:
        return LABEL_OVERRIDES[tool_id]
    description = re.sub(r"\s+", " ", tool["description"]).strip()
    first_sentence = re.split(r"[\u3002\uff01\uff1f]", description, maxsplit=1)[0].strip()
    return first_sentence.rstrip(";；,.，")


def load_catalog() -> list[dict[str, object]]:
    all_files = sorted(TOOLS_DIR.glob("*/*.yaml")) + sorted(TOOLS_DIR.glob("*/*.yml"))
    all_files = sorted(set(all_files))
    parsed_by_path = {path: read_tool_yaml(path) for path in all_files}
    path_by_id = {tool["name"]: path for path, tool in parsed_by_path.items()}
    file_by_domain: dict[str, list[Path]] = {}
    for path in all_files:
        file_by_domain.setdefault(path.parent.name, []).append(path)

    groups: list[dict[str, object]] = []
    selected_paths: list[Path] = []
    for group in TOOL_GROUPS:
        group_paths: set[Path] = set()
        for domain in group["domains"]:
            domain_files = sorted(file_by_domain.get(domain, []))
            if not domain_files:
                raise ValueError(f"No tool YAML found for domain: {domain}")
            group_paths.update(domain_files)

        for tool_id in group.get("include", []):
            if tool_id not in path_by_id:
                raise ValueError(f"Unknown included tool id: {tool_id}")
            group_paths.add(path_by_id[tool_id])
        for tool_id in group.get("exclude", []):
            if tool_id not in path_by_id:
                raise ValueError(f"Unknown excluded tool id: {tool_id}")
            group_paths.discard(path_by_id[tool_id])

        ordered_paths = sorted(group_paths, key=lambda path: (path.parent.name, path.name))
        selected_paths.extend(ordered_paths)
        groups.append(
            {
                "title": group["title"],
                "domains": group["domains"],
                "tools": [parsed_by_path[path] for path in ordered_paths],
            }
        )

    if set(selected_paths) != set(all_files):
        missing = sorted(str(path.relative_to(ROOT)) for path in set(all_files) - set(selected_paths))
        repeated = len(selected_paths) - len(set(selected_paths))
        raise ValueError(f"Tool grouping mismatch; missing={missing}, repeated={repeated}")

    tool_ids = [tool["name"] for group in groups for tool in group["tools"]]
    if len(tool_ids) != 300 or len(set(tool_ids)) != 300:
        raise ValueError(f"Expected 300 unique tools, got total={len(tool_ids)}, unique={len(set(tool_ids))}")
    return groups


def visual_width(text: str) -> float:
    return sum(0.58 if ord(char) < 128 else 1.0 for char in text)


def wrap_text(text: str, limit: float) -> list[str]:
    lines: list[str] = []
    current = ""
    for char in text:
        candidate = current + char
        if current and visual_width(candidate) > limit:
            lines.append(current.rstrip())
            current = char.lstrip()
        else:
            current = candidate
    if current:
        lines.append(current.rstrip())
    return lines


def wrap_function_list(labels: list[str], limit: float) -> list[str]:
    lines: list[str] = []
    current = ""
    for index, label in enumerate(labels):
        token = label + ("；" if index < len(labels) - 1 else "")
        if visual_width(token) > limit:
            if current:
                lines.append(current)
                current = ""
            chunks = wrap_text(token, limit)
            lines.extend(chunks[:-1])
            current = chunks[-1]
        elif not current or visual_width(current + token) <= limit:
            current += token
        else:
            lines.append(current)
            current = token
    if current:
        lines.append(current)
    return lines


class SVG:
    def __init__(self) -> None:
        self.parts: list[str] = []

    def add(self, value: str) -> None:
        self.parts.append(value)

    def rect(
        self,
        x: float,
        y: float,
        w: float,
        h: float,
        *,
        fill: str,
        stroke: str = "none",
        sw: float = 0,
        rx: float = 0,
        dash: str = "",
        opacity: float = 1.0,
    ) -> None:
        dash_attr = f' stroke-dasharray="{dash}"' if dash else ""
        self.add(
            f'<rect x="{x:.1f}" y="{y:.1f}" width="{w:.1f}" height="{h:.1f}" '
            f'rx="{rx:.1f}" fill="{fill}" fill-opacity="{opacity:.3f}" '
            f'stroke="{stroke}" stroke-width="{sw:.1f}"{dash_attr}/>'
        )

    def text(
        self,
        x: float,
        y: float,
        value: str,
        *,
        size: int,
        fill: str,
        weight: int = 400,
        anchor: str = "middle",
    ) -> None:
        self.add(
            f'<text x="{x:.1f}" y="{y:.1f}" text-anchor="{anchor}" '
            f'font-family="{FONT_STACK}" font-size="{size}" font-weight="{weight}" '
            f'fill="{fill}">{escape(value)}</text>'
        )

    def multiline(
        self,
        x: float,
        y: float,
        lines: list[str],
        *,
        size: int,
        fill: str,
        weight: int = 400,
        line_height: float = 1.5,
        anchor: str = "start",
    ) -> None:
        self.add(
            f'<text x="{x:.1f}" y="{y:.1f}" text-anchor="{anchor}" '
            f'font-family="{FONT_STACK}" font-size="{size}" font-weight="{weight}" fill="{fill}">'
        )
        for index, line in enumerate(lines):
            dy = 0 if index == 0 else size * line_height
            self.add(f'<tspan x="{x:.1f}" dy="{dy:.1f}">{escape(line)}</tspan>')
        self.add("</text>")

    def path(self, d: str, *, stroke: str, sw: float, opacity: float = 1.0) -> None:
        self.add(
            f'<path d="{d}" fill="none" stroke="{stroke}" stroke-width="{sw:.1f}" '
            f'stroke-opacity="{opacity:.3f}" stroke-linecap="round"/>'
        )

    def connector(
        self,
        d: str,
        *,
        stroke: str,
        sw: float,
        arrow: bool = False,
        dash: str = "",
    ) -> None:
        marker = ' marker-end="url(#arrowhead)"' if arrow else ""
        dash_attr = f' stroke-dasharray="{dash}"' if dash else ""
        self.add(
            f'<path d="{d}" fill="none" stroke="{stroke}" stroke-width="{sw:.1f}" '
            f'stroke-linecap="square" stroke-linejoin="miter"{dash_attr}{marker}/>'
        )

    def vertical_label(
        self,
        x: float,
        y: float,
        height: float,
        value: str,
        *,
        size: int,
        fill: str,
        weight: int = 400,
    ) -> None:
        chars = list(value)
        line_height = size * 1.25
        span = max(0, len(chars) - 1) * line_height
        first_baseline = y + height / 2 - span / 2 + size * 0.35
        self.multiline(
            x,
            first_baseline,
            chars,
            size=size,
            fill=fill,
            weight=weight,
            line_height=1.25,
            anchor="middle",
        )


def build_svg(groups: list[dict[str, object]]) -> tuple[str, int]:
    if sum(len(group["tools"]) for group in groups) != 300:
        raise ValueError("The function map must be generated beside the audited 300-tool catalog")

    accent = "#285F78"
    accent_dark = "#1D485D"
    light_blue = "#EAF3F7"
    light_gray = "#F3F5F6"
    page = "#FFFFFF"
    text_main = "#1F2D35"
    text_secondary = "#35454D"
    text_muted = "#60727B"
    height = 1950

    svg = SVG()
    svg.add(
        f'<svg xmlns="http://www.w3.org/2000/svg" width="{W}" height="{height}" '
        f'viewBox="0 0 {W} {height}" role="img" aria-labelledby="title desc">'
    )
    svg.add('<title id="title">AutoCarto 行军路线专题制图系统功能结构</title>')
    svg.add(
        '<desc id="desc">以一段制图文本为输入，展示制图知识抽取、底图数据加载、'
        '行军路线绘制、专题版面编排、质量反馈与版面导出的一级和二级功能。</desc>'
    )
    svg.add(
        '<defs><marker id="arrowhead" markerWidth="12" markerHeight="12" '
        'refX="10" refY="4" orient="auto" markerUnits="strokeWidth">'
        f'<path d="M0,0 L0,8 L11,4 z" fill="{accent}"/></marker></defs>'
    )
    svg.rect(0, 0, W, height, fill=page)

    # 系统标题和外部输入。
    root_w = 1760
    root_h = 150
    root_x = (W - root_w) / 2
    root_y = 38
    svg.rect(root_x, root_y, root_w, root_h, fill=light_blue, stroke=accent, sw=3.2, rx=34)
    svg.text(
        W / 2,
        root_y + 66,
        "AutoCarto 行军路线专题制图系统",
        size=54,
        fill=text_main,
        weight=500,
    )
    svg.text(
        W / 2,
        root_y + 122,
        "整体编排：编排智能体",
        size=34,
        fill=text_muted,
    )

    input_x, input_y, input_w, input_h = 70, 55, 500, 116
    svg.rect(input_x, input_y, input_w, input_h, fill="#FFFFFF", stroke=accent, sw=3.0, rx=22)
    svg.text(input_x + input_w / 2, input_y + 48, "制图任务输入", size=39, fill=text_main, weight=500)
    svg.text(input_x + input_w / 2, input_y + 93, "一段制图文本", size=34, fill=text_secondary)
    svg.connector(
        f"M {input_x + input_w:.1f} {input_y + input_h / 2:.1f} "
        f"L {root_x - 20:.1f} {input_y + input_h / 2:.1f}",
        stroke=accent,
        sw=3.2,
        arrow=True,
    )

    # 按二级功能数量给每个一级功能分配宽度。
    leaf_w = 125
    leaf_gap = 18
    group_gap = 52
    group_padding = 44
    group_widths = [
        max(590, len(phase["functions"]) * leaf_w + (len(phase["functions"]) - 1) * leaf_gap + group_padding)
        for phase in WORKFLOW_PHASES
    ]
    total_group_width = sum(group_widths) + group_gap * (len(group_widths) - 1)
    group_start_x = (W - total_group_width) / 2
    group_xs: list[float] = []
    cursor_x = group_start_x
    for group_w in group_widths:
        group_xs.append(cursor_x)
        cursor_x += group_w + group_gap
    group_centers = [x + w / 2 for x, w in zip(group_xs, group_widths)]

    trunk_y = 252
    header_y = 312
    header_h = 150
    branch_y = 525
    leaf_y = 575
    leaf_h = 730

    svg.connector(
        f"M {W / 2:.1f} {root_y + root_h:.1f} L {W / 2:.1f} {trunk_y:.1f}",
        stroke=accent,
        sw=3.0,
    )
    svg.connector(
        f"M {group_centers[0]:.1f} {trunk_y:.1f} L {group_centers[-1]:.1f} {trunk_y:.1f}",
        stroke=accent,
        sw=3.0,
    )

    for phase, group_x, group_w, center_x in zip(
        WORKFLOW_PHASES, group_xs, group_widths, group_centers
    ):
        svg.connector(
            f"M {center_x:.1f} {trunk_y:.1f} L {center_x:.1f} {header_y - 17:.1f}",
            stroke=accent,
            sw=2.5,
            arrow=True,
        )
        svg.rect(group_x, header_y, group_w, header_h, fill="#FFFFFF", stroke=accent, sw=2.7, rx=14)
        svg.text(
            center_x,
            header_y + 58,
            str(phase["title"]),
            size=45,
            fill=accent_dark,
            weight=500,
        )
        owner_lines = phase.get("owner_lines")
        if owner_lines is None:
            owner_lines = wrap_text("负责：" + str(phase["owner"]), group_w / 31)
        svg.multiline(
            center_x,
            header_y + 106,
            owner_lines,
            size=30,
            fill=text_muted,
            line_height=1.15,
            anchor="middle",
        )

        functions = list(phase["functions"])
        leaves_total_w = len(functions) * leaf_w + (len(functions) - 1) * leaf_gap
        first_leaf_x = group_x + (group_w - leaves_total_w) / 2
        leaf_centers = [
            first_leaf_x + leaf_w / 2 + index * (leaf_w + leaf_gap)
            for index in range(len(functions))
        ]
        svg.connector(
            f"M {center_x:.1f} {header_y + header_h:.1f} L {center_x:.1f} {branch_y:.1f}",
            stroke=accent,
            sw=2.4,
        )
        svg.connector(
            f"M {leaf_centers[0]:.1f} {branch_y:.1f} L {leaf_centers[-1]:.1f} {branch_y:.1f}",
            stroke=accent,
            sw=2.4,
        )
        for function_index, function_name in enumerate(functions):
            leaf_x = first_leaf_x + function_index * (leaf_w + leaf_gap)
            leaf_center = leaf_centers[function_index]
            svg.connector(
                f"M {leaf_center:.1f} {branch_y:.1f} L {leaf_center:.1f} {leaf_y - 16:.1f}",
                stroke=accent,
                sw=2.2,
                arrow=True,
            )
            svg.rect(leaf_x, leaf_y, leaf_w, leaf_h, fill="#FFFFFF", stroke=accent, sw=2.0)
            svg.vertical_label(
                leaf_center,
                leaf_y,
                leaf_h,
                str(function_name),
                size=48,
                fill=text_secondary,
                weight=400,
            )

    # 底部模块职责表：只概括核心责任，不重复上方的二级功能。
    table_x = 230
    table_y = 1340
    table_w = W - table_x * 2
    table_header_h = 78
    table_row_h = 98
    table_h = table_header_h + table_row_h * len(WORKFLOW_PHASES)
    col_module_w = 760
    col_count_w = 470
    col_responsibility_w = table_w - col_module_w - col_count_w
    table_border = "#C9D0D4"

    svg.rect(
        table_x,
        table_y,
        table_w,
        table_header_h,
        fill=light_gray,
        stroke=table_border,
        sw=1.5,
    )
    for row_index in range(len(WORKFLOW_PHASES)):
        row_y = table_y + table_header_h + row_index * table_row_h
        svg.rect(
            table_x,
            row_y,
            table_w,
            table_row_h,
            fill="#FFFFFF",
            stroke=table_border,
            sw=1.5,
        )

    table_bottom = table_y + table_h
    col_2_x = table_x + col_module_w
    col_3_x = col_2_x + col_responsibility_w
    svg.connector(
        f"M {col_2_x:.1f} {table_y:.1f} L {col_2_x:.1f} {table_bottom:.1f}",
        stroke=table_border,
        sw=1.5,
    )
    svg.connector(
        f"M {col_3_x:.1f} {table_y:.1f} L {col_3_x:.1f} {table_bottom:.1f}",
        stroke=table_border,
        sw=1.5,
    )

    header_baseline = table_y + 52
    svg.text(
        table_x + 30,
        header_baseline,
        "模块",
        size=35,
        fill=text_main,
        weight=500,
        anchor="start",
    )
    svg.text(
        col_2_x + 30,
        header_baseline,
        "核心职责",
        size=35,
        fill=text_main,
        weight=500,
        anchor="start",
    )
    svg.text(
        col_3_x + col_count_w / 2,
        header_baseline,
        "二级功能数",
        size=35,
        fill=text_main,
        weight=500,
    )

    for row_index, phase in enumerate(WORKFLOW_PHASES):
        row_y = table_y + table_header_h + row_index * table_row_h
        baseline = row_y + 63
        svg.text(
            table_x + 30,
            baseline,
            str(phase["title"]),
            size=34,
            fill=text_main,
            weight=500,
            anchor="start",
        )
        responsibility_lines = wrap_text(str(phase["responsibility"]), 61)
        responsibility_baseline = baseline - max(0, len(responsibility_lines) - 1) * 22
        svg.multiline(
            col_2_x + 30,
            responsibility_baseline,
            responsibility_lines,
            size=34,
            fill=text_secondary,
            line_height=1.28,
            anchor="start",
        )
        svg.text(
            col_3_x + col_count_w / 2,
            baseline,
            str(len(phase["functions"])),
            size=36,
            fill=text_main,
            weight=500,
        )

    svg.add("</svg>\n")
    return "\n".join(svg.parts), height


def write_catalog_csv(groups: list[dict[str, object]]) -> None:
    with OUT_CSV.open("w", encoding="utf-8-sig", newline="") as handle:
        writer = csv.writer(handle)
        writer.writerow(
            ["一级功能编号", "一级功能", "工具 ID", "二级功能", "当前状态", "运行类型", "原始分类", "定义文件"]
        )
        for index, group in enumerate(groups, start=1):
            for tool in group["tools"]:
                writer.writerow(
                    [
                        f"{index:02d}",
                        group["title"],
                        tool["name"],
                        concise_label(tool),
                        "待修复：执行绑定缺失" if tool["name"] in BROKEN_BINDINGS else "已接通",
                        tool["runtime"],
                        tool["domain"],
                        tool["path"],
                    ]
                )


def main() -> None:
    OUT_DIR.mkdir(parents=True, exist_ok=True)
    groups = load_catalog()
    svg_text, height = build_svg(groups)
    OUT_SVG.write_text(svg_text, encoding="utf-8")
    write_catalog_csv(groups)
    print(OUT_SVG)
    print(OUT_CSV)
    total = sum(len(group["tools"]) for group in groups)
    connected = sum(
        1 for group in groups for tool in group["tools"] if tool["name"] not in BROKEN_BINDINGS
    )
    print(f"size={W}x{height}; definitions={total}; connected={connected}")


if __name__ == "__main__":
    main()
