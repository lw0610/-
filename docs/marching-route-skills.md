# 行军路线制图所需 Skill：包含质量反馈

核对日期：2026-09-25。

范围：用户提供叙事文本，系统完成知识抽取、底图加载、制图排版、质量初审、修改、终审及成果导出。以下只列这条流程涉及的 skill，不扩展到统计图表、批量导图等其他功能。

按当前实现，清单分为 **4 个流程或审查 skill、5 个支撑工具包，知识抽取规则则直接来自智能体配置**。这不表示必须逐个调用全部 skill，也不是经过启动测试的最小配置方案；工具包提供工具，流程 skill 说明如何使用它们。

## 一、流程与审查 Skill

| Skill 文件 | 负责智能体 | 在这条流程中的作用 |
| --- | --- | --- |
| [marching-route-map-orchestration](<YOUR_WORKSPACE>/autocarto/skills/system/marching-route-map-orchestration/SKILL.md) | 编排智能体 `orchestrator` | 安排知识抽取、底图加载、制图、质量初审、修改和终审；检查各阶段是否完成 |
| [marching-route-data-visualization](<YOUR_WORKSPACE>/autocarto/skills/system/marching-route-data-visualization/SKILL.md) | 数据智能体 `data_engineer` | 指定底图原始数据源；检查并复用已加载的数据；加载缺少的图层。它不负责底图符号化 |
| [print-layout-marching-route](<YOUR_WORKSPACE>/autocarto/skills/system/print-layout-marching-route/SKILL.md) | 制图智能体 `cartographer` | 底图符号化；绘制路线、节点符号和图文标注；安排版面元素；导出图片和 PDF；核对并执行质量反馈中可行的修改 |
| [quality-feedback](<YOUR_WORKSPACE>/autocarto/skills/system/quality-feedback/SKILL.md) | 质量反馈智能体 `quality_feedback` | 初审真实地图图片和布局信息，检查遮挡、排版、对齐、文字、配色、可读性等；提出候选调整建议；终审对比初版与修改后的图片 |

质量反馈不是自动改图工具。它负责发现问题和提出建议，制图智能体负责判断建议是否符合制图规则，并执行可行的修改。

`quality-feedback` 的文件类型虽然是 `category`，但正文同时包含完整审查规则，因此这里归入“流程与审查”，不在工具包表中重复列出。

## 二、支撑工具包 Skill

| 工具包文件 | 本流程实际需要的能力 | 主要工具 |
| --- | --- | --- |
| [marching-route-knowledge-tools](<YOUR_WORKSPACE>/autocarto/skills/system/marching-route-knowledge-tools/SKILL.md) | 获取数据契约；将确认的抽取结果转换为六张制图数据表 | `marching_route_get_extraction_contract`；`marching_route_compile_dataset` |
| [project-state](<YOUR_WORKSPACE>/autocarto/skills/system/project-state/SKILL.md) | 查询当前工程、已加载图层和数据源 | `get_project_info`；`get_layers_info` |
| [data-loading](<YOUR_WORKSPACE>/autocarto/skills/system/data-loading/SKILL.md) | 加载底图图层 | 本流程主要使用 `data_load_vector`，不是把包中的四个工具全部执行 |
| [cartography-styling](<YOUR_WORKSPACE>/autocarto/skills/system/cartography-styling/SKILL.md) | 底图点线面符号化；依据六张表绘制路线、节点和图文标注 | `layer_set_marker_symbol`；`layer_set_line_symbol`；`layer_set_fill_symbol`；`cartography_build_and_draw_from_tables` |
| [print-layout](<YOUR_WORKSPACE>/autocarto/skills/system/print-layout/SKILL.md) | 创建版面；添加地图和配套元素；准备质量检查材料；调整版面及图文标注；导出图片和 PDF | `layout_create`、`layout_add_map`、`layout_add_*`、`layout_get_items_properties`、`layout_move_item`、`layout_move_callout`、`layout_export_image`、`export_layout_pdf` 等 |

上表 `layout_add_*` 表示多个添加元素的工具，不是一个可直接调用的工具名。

质量反馈所需的两个工具已包含在第一节的 `quality-feedback` 中：

- `prepare_quality_review`：初审时读取真实初版图片和布局快照，保存审查所需信息。
- `load_review_images`：终审时读取受保护的初版与修改后图片，供质量反馈智能体对比。

模板导入工具 `import_layout_template` 已包含在 `cartography-styling` 中。只有确实使用布局模板时才调用，不需要另加一个模板 skill。

## 三、知识抽取的规则来源

业务规则集中在知识智能体配置 `knowledge_engineer.prompts.static_instructions`，配置修改直接用于抽取和语义核对，并参与结果复用的兼容性检查。
专用运行时负责分段、原文证据、统一语义核对和人工确认；Python 知识工具负责数据契约与六表编译。原先不会加载的 `marching-route-knowledge-extraction` 说明包已移除。

## 四、包含质量反馈时的执行顺序

| 顺序 | 执行者 | 使用的说明或工具来源 |
| --- | --- | --- |
| 1. 编排任务 | 编排智能体 | `marching-route-map-orchestration` |
| 2. 抽取文本并转换六表 | 知识抽取专用程序 | `marching-route-knowledge-tools` 提供的两个工具及后端抽取逻辑 |
| 3. 检查、加载底图 | 数据智能体 | `marching-route-data-visualization` ＋ `project-state` ＋ `data-loading` |
| 4. 制作初版和审查材料 | 制图智能体 | `print-layout-marching-route` ＋ `cartography-styling` ＋ `print-layout` |
| 5. 质量初审 | 质量反馈智能体 | `quality-feedback`；使用 `prepare_quality_review` |
| 6. 核对建议、修改并导出 | 制图智能体 | 再次使用 `print-layout-marching-route`，调用 `print-layout` 中适用的调整和导出工具；没有可行修改时不强行改图 |
| 7. 质量终审 | 质量反馈智能体 | 再次使用 `quality-feedback`；使用 `load_review_images` 对比初版与修改后图片 |
| 8. 返回成果或未通过原因 | 编排智能体 | 按 `marching-route-map-orchestration` 汇总结果；终审未通过时不能宣称通过 |

初审和终审共用同一个 `quality-feedback`，不是两个不同 skill。当前规则中终审是终止检查，失败后报告剩余问题，不继续无限修改。

## 五、范围与文件位置

- 本清单不列“配置里存在，但这条正常流程不需要”的 skill。例如 `chart-visualization`、`map-series-batch-export`、通用数据清洗或坐标转换流程，不应为了凑全而加入。
- 用户确认、异常排查、另行指定数据处理等属于条件触发的辅助任务，不算上述正常流程的必经步骤。
- 底图符号化在制图阶段，不属于数据智能体的底图加载阶段。
- 已有完整六张制图数据表时，可跳过文本抽取；这里按用户从文本开始制作的情况整理。
- 该清单描述“启用质量反馈应该用哪些”，不是宣称最近那次实际执行已经进行过质量反馈。

Skill 所在目录：[autocarto/skills/system](<YOUR_WORKSPACE>/autocarto/skills/system)

当前默认智能体配置：[agent.yaml](<YOUR_WORKSPACE>/autocarto_data/config/agent_configs/default/agent.yaml)

本次只整理这份文档，未修改原有 skill、智能体配置或制图代码，也未启动制图任务。
