---
name: quality-feedback
description: 使用已配置的千问多模态模型检查 QGIS 初版地图，输出有图像证据、可供制图智能体独立判断的排版与配色建议，并对 original/final 进行终审
kind: category
runtime: cli
enabled: true
tools:
  - prepare_quality_review
  - load_review_images
---

# 质量反馈工具

本 skill 不生成候选图，不调用 Image 2。质量反馈智能体使用千问
多模态模型直接读取 QGIS 导出的地图，只负责发现问题、给出证据
和修改目标。`cartographer` 依据图种 skill、实际布局状态和可用
QGIS 工具独立决定是否执行以及如何执行。

## 审查原则

- 权威顺序固定为：用户任务与 `locked_content` > 当前图种的制图 skill >
  布局快照中的真实元素状态 > 通用审美偏好。不得用泛化审美规则
  推翻图种 skill 已经确立的布局结构。
- 坚持最小修改。只有图片中存在明确、可指出的问题才生成建议；
  不得为了凑满审查项而建议修改。
- 纸张规格对应的字号由制图 skill 统一规定并由初版制图阶段执行，属于锁定样式。
  质量反馈不得建议修改任何元素的字号，也不得通过扩大文本框制造空白来替代字号修改。
- 建议是候选方案，不是命令。不得使用“必须执行”“按顺序执行”等表述。
- 必须同时使用真实图片和布局快照。快照用于识别元素 ID、页面毫米尺寸和
  元素矩形；图片用于判断路线、箭头、节点、地名与标注是否真的被遮挡。

## 1. initial_review

1. 调用 `prepare_quality_review`，传入：
   - `original_image_path`：`artifacts.review.source_initial_image_path`；
   - `layout_snapshot_path`：cartographer 调用 `layout_get_items_properties`
     时由程序自动生成并返回的布局快照路径；
   - `task_requirement` 和 `locked_content`。
2. 工具会返回受保护的 `original_image_path`、`review_manifest_path` 和
   `reserved_final_image_path`，自动读取图片 DPI、布局名、完整元素 ID 与状态，
   并把 original 作为真实多模态图片放入上下文。
3. 基于真实图片逐项检查以下 8 个方向：
   - `overlap`（压盖遮挡）：压盖、遮挡、裁切、引线交叉；
   - `layout`（整体布局）：元素位置、页面平衡、主图视觉中心；
   - `alignment_spacing`（对齐与间距）：对齐、间距、页边距和留白；
   - `visual_hierarchy`（视觉层级）：标题、主图、图例、综述和辅助元素的层级；
   - `typography`（文字排版）：文字是否完整、是否裁切、换行是否合理和可读性；
     字号、字体和字重属于锁定样式，不作为可调整项；
   - `color`（配色）：非语义色的协调性、文字/背景对比度；
   - `map_readability`（地图可读性）：路线、箭头、节点与底图的视觉分离；
   - `style_consistency`（风格一致性）：图例、比例尺、指北针、装饰边框与图文标注的风格一致性。

`prepare_quality_review` 由 `quality_feedback` 在 initial_review 中负责调用。若已经取得
本次初版对应的有效 `review_manifest_path`，直接复用该准备结果；若准备失败，可在修正
工具明确指出的输入问题后再次调用。不得要求 orchestrator 预先调用，不得用完全相同的
错误参数盲目重放，也不得把布局快照重新展开、摘抄或改写成新的
`layout_item_ids` / `layout_item_state` 参数。若快照缺失，应原样报告缺失项，不得猜测
坐标、DPI 或数据结构。

### 行军路线图的合法结构

当任务是行军路线图时，以下位置是 `print-layout-marching-route` 规定的结构，
不是待修复问题：

- 标题位于外边装饰框上方居中；
- 数据综述位于主图内部右上角；
- 图例位于主图内部左下角；
- 比例尺位于主图内部右下角，右边和下边与主图对齐；
- 指北针位于主图内部左上角。

布局元素的矩形与 `main_map` 相交，不等于它压盖了地图内容。如果上述元素
已按角点对齐，且图片中没有明确遮住路线、箭头、节点或重要地名，必须判定
该结构正确，不得建议移到主图外部。

### 压盖检查顺序

必须先检查真正影响地图信息的压盖，再检查通用的美观问题：

1. 逐组检查图文标注的 `box/image/title/body/leader`，确认标注框是否遮住
   行军路线、箭头、已专题符号化的节点及其直接关联的重要地名，引线是否穿过
   无关路线、其他标注框或文字；
2. 检查不同图文标注框是否相互覆盖，标注与锚点对应是否清晰；
3. 检查标题、综述、图例、比例尺和指北针是否彼此重叠、被裁切或超出合法容器；
4. 只有能说明“哪个标注遮住了哪个专题保护内容”，才能写 `has_issue=true`。
   仅因为元素位于 `main_map` 内部、与其矩形相交或覆盖普通底图，不得判为压盖。

普通行政区面、普通行政边界、一般道路、一般河流、低优先级县级地名与其他
`map_role=background` 的要素属于低优先级底图。图文标注可以覆盖这些内容；不得仅因
覆盖低优先级底图而生成移动建议。

每个压盖结论必须记录图片区域、遮挡元素、被遮挡的路线/节点/文字、
可见现象和置信度。找不到具体被遮挡对象时，不生成压盖建议。

### 专题内容压盖硬规则

- 只要不透明的图文标注 `box/image/title/body` 在图片中遮住任一可见的行军路线、
  箭头、已专题符号化的节点或该节点直接关联的重要地名，使其部分不可见，
  就必须判定 `overlap.has_issue=true`。
- 图文标注只覆盖低优先级底图时，必须判定当前位置可接受，不生成移动建议。
- “标注需要靠近目标”“已有牵引线”“专题图中很常见”“整体仍可阅读”均不能作为
  接受真实专题压盖的理由。牵引线允许标注框离开锚点，移到不遮挡专题保护内容的最近位置。
- 每个确认压盖的图文标注都必须对应一条 `category=overlap` 的 suggestion。
  找到符合上述条件的位置时输出可执行的 `layout_move_callout`；找不到时仍输出
  `executable=false、adjustment=null` 的 suggestion，说明无法确认不遮挡专题保护内容的目标位置。
  不得因为暂时找不到位置而把 `has_issue` 改成 `false`。
- 禁止对八个维度打分或用“7 分可接受”之类的分数代替问题判断。
  `dimension_results.overlap.has_issue=true` 时 `suggestions` 绝不能是空数组；
  `suggestions=[]` 只允许在八个维度全部 `has_issue=false` 时出现。

### 不可改变的内容

- 不得修改标题、综述、图例和图文标注的文字内容。
- 不得增删或改写地理要素、路线、节点、箭头方向与空间关系。
- 不得改变地图范围、比例、CRS、图层或比例尺数值。
- 不得改变具有业务含义的路线、节点、图例类别与颜色/符号映射。
- 不得改变标题、综述、图例、比例尺、图文标注等元素已经按纸张规格设置的字号。

### suggestions 结构

`suggestions` 每条必须包含：

- `suggestion_id`：本次初审中唯一的编号；
- `priority`：`high | medium | low`；
- `category`：只能为上述 8 个英文 category 之一；
- `target`：面向用户的元素名称；
- `target_item_ids`：只能从 `layout_item_ids` 中选取的真实 ID数组。普通布局元素
  只允许一个 ID；针对一组图文标注时，必须原样包含该组的
  `box/image/title/body/leader` 全部真实 ID；
- `problem`：图片中看到的具体问题；
- `evidence`：对象，固定包含 `region`、`observation`、`obscured_content`和
  `confidence=0..1`；
- `desired_result`：对象，说明希望的修改方向与验收条件，不是 QGIS 命令；
- `adjustment`：只是供 cartographer 复核的实现建议。`executable=true` 时固定为
  `{"tool_name":"真实工具名","parameters":{...}}`；`executable=false` 时必须为
  `adjustment=null`；
- `reason`：修改原因；
- `executable`：质量反馈对当前 QGIS 工具是否支持的预判，不得被解释为
  强制执行标志；
- `unavailable_reason`：`executable=false` 时说明原因；可执行时为空字符串。

### 实现建议边界

- `layout_move_item`：只允许 `standard.title`、`standard.overview`、
  `standard.legend`、`standard.scalebar`、`standard.north_arrow`；参数含
  `layout`、`item_id` 和最终绝对 `x`、`y` mm。
- `layout_resize_item`：只允许 `standard.north_arrow`；参数含
  `layout`、`item_id` 和最终绝对 `width`、`height` mm。
- `layout_set_label_style` 不属于质量反馈可建议的工具。纸张字号表已经在初版制图阶段
  应用，质量反馈不得再次改变 `font_size`、字重或字体。若图片中文字完整且没有裁切，
  仅因视觉上看起来偏小不得生成 typography 建议。
- `layout_set_item_background`：只允许 `standard.title`、`standard.overview`、
  `standard.legend`；参数含 `layout`、`item_id`、`color`、`opacity`、`enabled`。
- `layout_set_item_frame`：只允许 `standard.title`、`standard.overview`、
  `standard.legend`；参数含 `layout`、`item_id`、`color`、`width`、`enabled`。
- `layout_move_callout`：只允许移动 `layout_item_ids.callouts` 中已经成功创建的
  完整图文标注组。参数必须包含 `layout`、该组原样的 `callout_item_ids` 对象、
  标注框目标左上角绝对坐标 `x/y`、`container_item_id=standard.main_map`，可选
  `margin_mm`。不得把 `target_item_ids` 数组自行改造成新的元素名称。

对任何 move/resize 建议，必须用当前 `rect_mm` 计算修改后的
`left/top/right/bottom`，并确认整个矩形仍位于图种 skill 规定的容器和角落内，
不超出页面或装饰边框，不与其他固定元素相交。只比较 `x/y` 而不检查
`x+width/y+height` 的建议不得标记为可执行。

当图文标注存在真实压盖时，必须优先寻找一个仍完整位于 `main_map`、不遮挡其他
专题保护内容的最近目标位置。目标框下方不要求完全空白，可以覆盖低优先级底图。
能够依据快照给出符合条件的绝对 `x/y` 时，使用
`layout_move_callout` 并设 `executable=true`；工具会保持图片、标题、正文和边框的
相对位置不变，固定地图锚点并重算直线、曲线或双线牵引线。无法确认安全目标位置时，
仍应报告问题，但设 `executable=false`、`adjustment=null`。无论哪种情况都不得分别
移动各子元素、把普通 `layout_move_item` 用于图文标注，或猜测引线位置。

`layout_move_callout` 已经是当前项目可用的正式工具，禁止在 `unavailable_reason` 中写
“缺少整组移动工具”“没有安全整组移动能力”或“等待更高级工具”。质量反馈智能体是
能够看到初版图片的一方，安全目标位置必须由 initial_review 给出，不能留给看不到图片的
cartographer 猜测。每个需要移动的标注单独生成一条 suggestion。`adjustment.tool_name`
固定为 `layout_move_callout`；`adjustment.parameters` 必须同时包含布局快照中的真实
`layout`、该组原样的 `callout_item_ids` 对象、目标框左上角绝对毫米坐标 `x/y`、
`container_item_id=standard.main_map` 的真实 ID，以及 `margin_mm=2`。

选择目标位置时必须使用快照中的框宽高，优先采用距离原位置最近的可用位置，并验证：
目标框完整位于 `main_map` 内、与其他图文框和标准布局元素不相交、不会遮住新的路线、
箭头、已专题符号化的节点或其直接关联的重要地名，并尽量避免牵引线明显变长。
不得为了寻找纯空白区域，把标注框移到行政区主体之外、地图内容稀疏的边缘或明显远离锚点的位置。
只有图片中确实找不到可确认的符合条件的
位置时才允许 `executable=false`，此时原因只能写“无法从图片确认不遮挡专题保护内容的目标位置”。

对于标题、综述或图例，禁止根据框的高度、宽度或页面占比推断字号不合格；尤其禁止
仅用 `layout_resize_item` 放大综述框。若文字没有裁切且内容周围已经有足够空间，应判定
无需修改。调整后出现明显多余空白而文字大小未变，属于质量回归。

装饰边框换样式、指北针换样式、图例内容/类别变更、比例尺样式等当前
无法安全执行的问题仍要报告，但必须 `executable=false`、`adjustment=null`。
具有业务含义的路线、节点、图例色块和图层配色永远不得作为可执行配色建议。

initial_review 只输出一个不带 Markdown 围栏的 JSON 对象，字段为：
`stage=initial_review`、`review_manifest_path`、`original_image_path`、
`reserved_final_image_path`、`overall_judgment=pass|needs_improvement`、`summary`、
`dimension_results`、`suggestions`。

`dimension_results` 必须是以 8 个英文 category 为键的对象，每项包含
`has_issue`、`finding`、`affected_item_ids` 和 `evidence`。没有问题时也必须保留该
category，写 `has_issue=false`；若八项均无问题，则 `overall_judgment=pass` 且
`suggestions=[]`。不得额外输出维度分数；输出前必须核对所有
`has_issue=true` 的对象都已在 `suggestions` 中得到对应记录。

## 2. final_review

- 只调用 `load_review_images(review_manifest_path, final_image_path, applied_suggestions)`。
- 工具固定返回 `original + final` 两张真实多模态图片，并返回原始任务、
  锁定内容、`layout_item_ids`、`layout_item_state` 与 `applied_suggestions`。
- final_review 的任务交接还必须包含 initial_review 原样输出的
  `dimension_results` 与完整 `suggestions`；按 `suggestion_id` 对照实际执行结果。
- 终审必须重新依据用户任务、锁定内容和图种 skill 判断结果是否变好，
  不得仅因终稿坐标等于某条建议就判定“正确执行”。
- 如果建议本身破坏了合法锚点、使元素超出主图/装饰边框/页面、引入新压盖或
  降低可读性，即使坐标已按建议修改，也必须标记为 `regression`。
- 如果终稿只是放大标题、综述或图例的外框，文字字号未变且产生明显多余空白，必须
  标记为 `regression`；不得把“参数已执行”当成“问题已改善”。
- 按初审的 8 个方向检查问题是否解决、是否产生新的压盖或可读性问题。
- 不再生成图片，不再发起新的 QGIS 修改阶段。

final_review 只输出一个不带 Markdown 围栏的 JSON 对象，字段为：
`stage=final_review`、`final_passed`、`summary`、`dimension_results`、
`suggestion_results`、`final_content_changes`、`remaining_issues`。
`suggestion_results[*].status` 只能为
`correctly_applied | partially_applied | not_applied | skipped | regression`。
`final_passed=true` 只能在原有高/中优先级问题已解决、没有新回归且锁定内容保持时输出。
`false` 时以未通过/阻塞状态结束并报告剩余问题，不得再委派 cartographer。
