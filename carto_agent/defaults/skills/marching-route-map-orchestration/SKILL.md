---
name: marching-route-map-orchestration
description: 行军路线图多智能体编排流程约束。输入包含叙事文本时按 knowledge_engineer -> data_engineer -> cartographer -> orchestrator 执行；已有六表数据时从 data_engineer 开始，并要求每个阶段输出结构化交接结果。
kind: knowledge
---

# 行军线路图多智能体编排

## 触发条件

当用户请求包含以下意图时，必须启用本流程：

- 制作、生成、绘制、输出某战斗的行军线路图
- 制作战斗线路图、行军路线图、军事专题线路图
- 对行军路线图进行整理后再制图导出

示例：`制作一个河南省行军线路图`、`帮我做一张河南省行军线路布局图`。

## 总体规则

- 不要让单个子智能体吞掉全流程。
- `orchestrator/planner`不考虑CRS，只负责阶段调度和交接检查。
- 主智能体只负责阶段调度、交接检查和最终汇总，只告诉对应的子智能体按照流程完成自己的任务，不进行额外的指导。
- 具体工具由对应子智能体根据自己的可用工具和上下文选择。
- 每个阶段结束后必须输出结构化交接结果，再决定是否进入下一阶段。
- 交接结果是阶段契约，不是可选总结。orchestrator 必须检查
  `status`、`artifacts.dataset_source`、`artifacts.dataset_dir` 和
  `artifacts.table_counts`；必需字段缺失时只允许要求原子智能体补充一次，
  不得自行猜测或从 `output_get_info` 反查。
- 如果任一阶段返回 `status=blocked` 或 `blocking_issues` 非空，回到 orchestrator，由 orchestrator 决定补充上下文、请求用户确认或终止任务。
- 用户给出史料、路线叙事或自然语言事件描述时，必须先进入 knowledge_engineer；不得让 data_engineer 或 cartographer 直接从文本猜六张 CSV。
- 用户已经明确给出完整六表 `dataset_dir` 时，knowledge_engineer 可跳过，必须在交接中说明使用的是既有数据集。
- “制作一张河南省行军路线图”只是制图意图，不是路线叙事；没有具体主体、地点、路线关系或现有六表时，禁止编造史料并启动知识抽取。

## 本流程的 CRS 执行边界

- 八个底图源文件的 CRS 不一致是已知、可接受的输入状态，不是待修复错误。QGIS 负责显示时的动态坐标转换。
- `data_engineer` 只读取并报告每个原始图层的 CRS；严禁重投影、定义/修改 CRS、另存、导出、格式转换或创建任何投影副本。
- 禁止为该流程调用 CRS 转换工具、处理算法或 `scripting_specialist`；不得生成名称带 `_3857`、`_proj`、`_src`、`_unified`、`_export` 或 `_lcc` 的底图副本。
- orchestrator/planner 不得把“CRS 不一致”追加成统一坐标系、重投影、导出副本或重试任务，也不得要求子智能体执行这些操作。
- 交接中的 `artifacts.crs` 只是诊断报告，不是后续整改清单；只要八个原始源可用，CRS 差异不得使阶段进入 `blocked` 或触发重试。

## 固定阶段

### 0. knowledge_engineer（有叙事文本时必需）

- 当前用户提供史料时，只委派一次，`task.description` 为 `{"source_ref":"current_user_message"}`；运行时读取完整原文，编排模型不转述或摘要。
- 只有明确提供原文的兼容调用使用 `source_text`；可选输出或素材路径须来自用户或工具确认。用户明确要求重抽时传独立布尔字段 `force_refresh:true`。只说“重新抽取”而无法选定原文时，先确认待重抽文本。
- 专用运行时使用知识智能体配置中的业务规则，完成按预算抽取、程序整理、自动补坐标、结果编辑确认和六表编译；抽取后不再调用大模型全文语义核对；编排智能体不直接调用知识工具或自行修补图谱。
- 抽取结果保存后允许用户直接开始制图或在已有编辑页修改；不追加模型核对步骤。明确失败时报告原因，不反复委派。坐标可先待定，生成地图所需地点必须完成定位。
- 成功后原样交接 `dataset_source=current_session`、`artifact_id`、`dataset_dir`、`task_id`、`task_graph_file`、`source_file`、`counts`、`validation` 和 `warnings`，不得猜测路径或复用旧会话的数据目录。
- 完整原文、规则和模型匹配时可复用兼容结果，但重新编译本会话六表；保留人工修改、来源和警告。过程记录由运行时保存，不由模型重建。

### 1. data_engineer

职责：

- 加载知识抽取阶段生成的 `dataset_dir`，或用户明确提供的现有六表数据集。
- 进行行军路线图底图数据加载与检查；路线点符号化由后续统一制图入口执行。

- 必须调用并严格遵循 `marching-route-data-visualization` skill 加载底图。该 skill 的允许清单是图层名称和源路径的唯一事实来源；本编排 skill 不再重复维护路径，也不得回退到旧河南底图。

- 如缺少关键数据，明确报告缺口，不得假装数据存在。
- 只允许使用 `marching-route-data-visualization` 允许清单中的 8 个原始 `.shp` 源作为底图，不得加载它们的任何转换或导出副本。
- 加载前按规范化后的绝对源路径检查 QGIS 工程：如果同一原始源已加载，复用该图层；只加载缺失的原始源。不得仅因名称、样式或 CRS 不同而重复加载。
- 交接中必须单独返回 `artifacts.original_layers`，逐层记录 `name`、`source_path`、`layer_id`、`crs` 和 `load_status=loaded|reused`；后续只使用这个清单作为底图输入。

完成条件：

- 图层清单、范围、数据缺口说明。
- `artifacts.original_layers` 恰好对应 `marching-route-data-visualization` 允许清单中的 8 个原始源，不包含转换副本、重复源或其他工程图层。
- 只报告 CRS 差异，不对任何图层执行 CRS 处理，不把 CRS 差异列入 `blocking_issues`。
- 向 cartographer 原样传递上游 `dataset_dir`；不得替换为写死的旧目录。
- data_engineer 的返回中如果缺少 `artifacts.dataset_source`、
  `artifacts.dataset_dir` 或 `artifacts.table_counts`，本阶段不算完成。

### 2. cartographer

职责：

- 进行行军线路图可视化与布局制作。
- 底图必须严格使用 data_engineer 交接的 `artifacts.original_layers`；不得扫描并把工程内其他同名、后缀或投影副本加入布局。
- 不得因底图 CRS 不同请求重投影；使用 QGIS 动态坐标转换展示原始图层。
- 叙事知识抽取流程调用绘制入口时使用
  `dataset_source=current_session`，让程序直接解析当前任务数据集；
  不得调用 `output_get_info` 反查路径，也不得改用历史或示例数据。
- 只有用户一开始就明确提供完整六表目录时，才使用
  `dataset_source=explicit` 和该 `dataset_dir`。

完成条件：
- 完整符号表映射的点符号化。
- 完整的行军线路图布局。
- 输出包含主图、标题、网格、路线、符号、图文标注框、图例、数据综述、比例尺、指北针的可交付专题图。
- 产出地图布局名称、导出文件路径、主要图层和制图检查结果。
- 启用质量反馈时，把绘制工具与各 `layout_add_*` 返回的真实 ID 直接交给
  `layout_get_items_properties` 生成布局快照，阶段交接只返回
  `artifacts.review.layout_snapshot_path`，不得在后续阶段展开、摘抄或重建完整状态。

### 2.5 quality_feedback（仅当用户明确启用质量反馈）

- cartographer 初版交接使用公共 envelope：布局名写入 `artifacts.layout`，
  `layout_export_image` 成功结果的 `output` 重命名为
  `artifacts.review.source_initial_image_path`，并同时交接 `locked_content`、
  原始任务和 `artifacts.review.layout_snapshot_path`。必须先创建布局及主图、
  完成图文标注和全部布局元素，再由 `layout_get_items_properties` 自动保存快照，
  并按 A0=100、A1=140、A2/A3/A4=200 选择审查 DPI 导出初版；
  终稿 PNG 必须保持相同页面尺寸和实际审查 DPI。
- `locked_content` 不得为空，至少固定包含：所有文字原文；路线、节点、方向与拓扑；
  地图范围、比例、CRS 和图层；图例类别、符号与颜色语义映射；比例尺数值与指北方向。
  还必须包含当前纸张规格对应的字号表，质量反馈阶段禁止修改任何 `font_size`。
  用户另有禁改要求时只能追加，不能删除这些基础锁定项。
- orchestrator 严禁直接调用 `prepare_quality_review`，只负责把
  `mode=initial_review`、初版路径、布局快照路径、原始任务和锁定内容原样委派给
  `quality_feedback`。不得把快照内容改写成自然语言摘要。
- `quality_feedback` 在 `initial_review` 中负责调用 `prepare_quality_review`；已有本次初版
  对应的有效 `review_manifest_path` 时直接复用，准备失败时可在修正工具指出的输入问题后
  再次调用，禁止以相同错误参数盲目重放。该工具保护初版、建立质量反馈清单、
  预留终稿路径，并把受保护的 original 作为真实图片交给千问多模态模型；不调用
  Image2，也不生成 candidate。调用时明确映射
  `original_image_path = artifacts.review.source_initial_image_path`、
  `layout_snapshot_path = artifacts.review.layout_snapshot_path`，并传入原始任务和锁定内容。
  图片 DPI、布局名、完整 ID 与状态由程序读取，不得由智能体填写或猜测。
- quality_feedback 按压盖遮挡、整体布局、对齐与间距、视觉层级、文字排版、配色、
  地图可读性和风格一致性八个方向检查 original。`dimension_results` 的每项只能包含
  `has_issue`、`finding`、`affected_item_ids` 和 `evidence`，禁止打分或以“分数尚可”
  代替问题判断；orchestrator 委派时也不得要求“各维度评分”。压盖检查必须优先覆盖
  每组图文标注与行军路线、箭头、已专题符号化的节点及其直接关联的重要地名、其他标注的真实关系；
  只有能够说明“哪个标注遮住了哪个专题保护内容”才能判定有问题。普通行政区面、普通行政边界、
  一般道路、一般河流、低优先级县级地名和其他 background 要素允许被图文标注覆盖，不得仅因此生成移动建议。
  只要不透明图文框、图片、标题或正文遮住任一可见行军路线、箭头、已专题符号化的节点或该节点直接关联的重要地名，
  就必须令 `overlap.has_issue=true`；“靠近目标”“已有牵引线”“地图中常见”或“整体仍可读”均不能作为接受真实专题压盖的理由。顶层 JSON 是阶段
  专用输出，orchestrator 必须将其原样映射到 `artifacts.review`。当前项目已经提供
  `layout_move_callout`，不得报告“缺少整组移动工具”；quality_feedback 必须为可移动的每一组标注给出
  距离原位置最近且不遮挡专题保护内容的目标 `x/y`与完整工具参数，不要求目标框下方完全空白。
  不得为寻找纯空白区域把标注框移到行政区主体之外、地图内容稀疏的边缘或明显远离锚点的位置，也不能把坐标选择转交给 cartographer。
- `suggestions` 是 cartographer 需要独立复核的候选方案，不是强制命令。orchestrator
  必须原样传递完整 `suggestions`、`dimension_results`、路径、ID、参数与单位，禁止删减、
  改写、重新计算或转述为“必须执行”“按顺序执行”。`executable=false` 的问题保留在
  报告中，不得重新解释成“评估并尝试”，也不得降级成文本框、猜测 ID、虚构工具或
  修改锁定内容。每个确认压盖的图文标注必须对应一条 `category=overlap` 的 suggestion；
  暂时无法确认安全目标位置时也必须保留该 suggestion，并设 `executable=false`、
  `adjustment=null`，不得把问题改判为不存在。任一维度 `has_issue=true` 时
  `suggestions` 不得为空；`suggestions=[]` 只允许八项全部为 false。若审查文字已经说明
  存在压盖但 suggestions 为空，该交接自相矛盾，orchestrator 不得进入 cartographer，
  必须要求 quality_feedback 按原审查结论补齐结构化建议。任何改变字号或仅放大文本框
  的建议均视为违反锁定规则，不得下发。
- cartographer 只评估 `executable=true` 的候选建议。执行前必须重新核对图片证据、
  图种 skill 的合法锚点、真实 ID、工具参数和修改后的完整矩形；不成立、越界、产生
  新压盖或破坏图种结构时必须 `skipped`。图文标注整组问题只允许使用
  `layout_move_callout`，并原样传递该组 `created_item_ids`；几何检查通过时不得以
  “看不到图片”或“没有工具”为由跳过，不得分别移动子元素。
  随后把 QGIS 终稿 PNG 导出到 `reserved_final_image_path`，且使用
  与初版相同的布局、页面尺寸和 DPI；
  不得覆盖受保护 original。所有修改完成后连续导出
  终稿 PNG 和 PDF，中间不得再修改布局，并返回 `final_image_path`、
  `final_pdf_path` 和 `applied_suggestions`。
- `final_review` 的工具参数只传 `review_manifest_path`、`final_image_path`和
  `applied_suggestions`；任务交接同时保留原始任务、锁定内容、
  initial_review 的 `dimension_results` 与完整 `suggestions`，供终审逐项核对。
  工具固定把 `original + final` 两张真实图片交给千问复检。
- final_review 是终态：`final_passed=true` 时完成；`false` 时以未通过/阻塞
  状态结束并报告 `remaining_issues`。两种结果都不得再委派 cartographer。

### 3. orchestrator

职责：

- 检查所有阶段交接结果。
- 检查 `original_layers` 是否恰好指向 8 个允许的原始源。对 CRS 差异只做汇总，严禁追加“统一 CRS”、“生成投影副本”或“再次导出”任务。
- 输出目录为 AutoCarto 当前运行的 `exports` 子目录：`<YOUR_WORKSPACE>/autocarto_data/data/sessions/<当前会话>/<当前运行ID>/exports/`。
- 汇总用户可见结果，包括已完成操作、输出路径、失败/降级原因和剩余风险。
- 未启用质量反馈时，仅在最终检查发现明确缺失项时才可请求对应阶段补充；
  启用质量反馈时，final_review 为终态，不再启动任何专业子智能体。

## 交接结果格式

每个子智能体结束时必须返回以下 JSON 或等价结构化内容：

```json
{
  "status": "completed | skipped | blocked",
  "agent": "knowledge_engineer | data_engineer | cartographer | quality_feedback",
  "summary": "本阶段完成了什么",
  "artifacts": {
    "layers": [],
    "original_layers": [
      {
        "name": "原始图层名",
        "source_path": "原始 .shp 绝对路径",
        "layer_id": "QGIS 图层 ID",
        "crs": "只读诊断值",
        "load_status": "loaded | reused"
      }
    ],
    "files": [],
    "dataset_source": "current_session | explicit",
    "artifact_id": "",
    "dataset_dir": "",
    "task_id": "",
    "task_graph_file": "",
    "table_counts": {},
    "layout": "",
    "review": {
      "source_initial_image_path": "",
      "layout_snapshot_path": "",
      "task_requirement": "",
      "locked_content": [
        "所有文字原文",
        "路线、节点、方向与拓扑",
        "地图范围、比例、CRS 和图层",
        "图例类别、符号与颜色语义映射",
        "比例尺数值与指北方向"
      ],
      "review_manifest_path": "",
      "original_image_path": "",
      "reserved_final_image_path": "",
      "final_image_path": "",
      "final_pdf_path": "",
      "stage": "",
      "overall_judgment": "",
      "summary": "",
      "dimension_results": {},
      "suggestions": [],
      "applied_suggestions": [],
      "final_passed": false,
      "suggestion_results": [],
      "final_content_changes": [],
      "remaining_issues": []
    },
    "extent": "",
    "crs": ""
  },
  "next_agent": "knowledge_engineer | data_engineer | cartographer | quality_feedback | orchestrator",
  "blocking_issues": []
}
```

`next_agent` 决策规则：

- `knowledge_engineer` 成功后交给 `data_engineer`。
- `data_engineer` 成功后交给 `cartographer`。
- 未启用质量反馈时，`cartographer` 成功、跳过或阻塞后交回 `orchestrator`。
- 启用质量反馈时，初版 cartographer 交给 `quality_feedback`；initial_review
  交回 cartographer；终稿 cartographer 再交给 `quality_feedback`；final_review
  完成后交回 `orchestrator`。`final_passed=false` 时 orchestrator 只能报告
  未通过与 `remaining_issues`，不得宣称任务已成功。
- 任何阶段阻塞时，交回 `orchestrator`。
- 进入下一阶段前，orchestrator 必须验证上一阶段交接必需字段非空；
  不能把自然语言汇报视为等价的结构化交接。
- data_engineer 交给 cartographer 前，`original_layers` 必须有且仅有 8 项，每项 `source_path` 必须与本 skill 的允许清单一致。如有缺失只报告缺失源；不得用转换副本替代。

## 河南省行军线路图默认理解

当用户只说“制作一个河南省行军线路图”且没有提供更多细节时，采用以下默认理解：

- 地理范围：河南省行政范围。
- 目标产物：行军线路专题图，优先 PDF 导出。
- 底图优先级：当前工程内已加载的同一允许原始源 > 按本 skill 给定绝对路径加载缺失原始源 > 报告原始源缺失。工程内其他图层不进入本流程底图清单。
- 需要先检查数据，不得直接进入布局导出。

## 失败处理

- 知识阶段局部 JSON、证据、引用或关联方案非法时，专用运行时带明确错误重试一次；
  局部输出截断或上下文不足最多细分两级。仍失败、无句末超长片段无法安全处理、
  关联请求超限、主体/路线疑问未解决或六表编译失败时，返回 blocked 并保留过程记录。
- knowledge_engineer 的空结果、blocked 或运行错误不得由 orchestrator 重试或接管；
  停止并保留原始错误及 `trace_file`，不以旧图谱或数据集降级。
- 当前方案只保证执行相邻块联合核对，不保证远距离关系或真实模型语义正确。
  原文引用避免模型重抄全文，但输入本身仍受主模型上下文限制，不承诺任意长文本。
- 缺少数据时，停止在 `data_engineer` 阶段，向用户说明缺少哪些图层或文件。
- 仅有原始源文件不存在或无法加载才能作为底图阻断项；CRS 不同、工程中存在旧副本或非本流程图层均不得触发重投影或重试。
- 其他阶段工具失败时最多基于错误信息修正重试一次；知识阶段遵守上述专用运行时规则。
- `route_dataset_handoff_conflict`、`route_dataset_*_mismatch` 或
  `route_dataset_validation_failed` 属于数据来源错误，必须停止并交回
  orchestrator；禁止使用旧数据目录降级。
- 布局 workflow 失败时，遵守 `cartographer` 中关于 `print-layout-thematic` 的降级规则。
- 质量反馈的 final_review 失败或 `final_passed=false` 时直接返回未通过结果；
  这是终态验收结果，后续不再修改 QGIS 布局。
