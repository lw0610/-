---
name: print-layout-marching-route
description: 行军线路图可视化与布局制作，按主图四角进行刚性对齐，输出包含主图、标题、贝塞尔曲线、军标箭头、符号、图文标注框、图例、数据综述、比例尺、指北针的可交付专题图
kind: workflow
---

# 行军线路图布局制作流程（四角对齐版）

## 目标

本流程用于制作行军线路图，并强制以主图边界为锚点进行要素对齐：

- 执行路线绘制、点符号化和图文注记
- 图例左下角与主图左下角对齐
- 综述右上角与主图右上角对齐
- 指北针左上角与主图左上角对齐
- 比例尺条带目标宽度为 80mm，最终元素外框宽度以 QGIS 返回的实际 `data.rect_mm` 为准；右下角与主图右下角对齐
- 装饰边框包裹主图边界，外框相对主图四边各外扩 5mm，总宽高分别比主图多 10mm；不得改变主图的位置、尺寸、比例尺或范围
- 标题位于装饰图框上方，按图框宽度水平居中，字体加粗

- 已有行军相关图层（地级城市驻地/行政区界等）
- 已确定输出纸张（推荐 A1 横向）

## 严格执行约束（必须）
- 不得省略：主图、标题、、图例、数据综述、比例尺、指北针。
- 主图、标题、图例、数据综述、比例尺、指北针在布局中不得重复添加。
- 必须调用 `cartography_build_and_draw_from_tables` 自动构建并执行路线绘制、点符号化和图文注记。
- 必须调用 `layout_add_decorative_border` 为主图生成装饰边框；边框只能由 `main_map` 的四角 bounds 向外扩展计算，不能作为新的对齐基准。
- 必须先创建布局和 `main_map`，再调用 `cartography_build_and_draw_from_tables` 生成路线、符号和图文标注；图文标注依赖已有布局与主图，严禁换序。
- 必须在所有元素添加完成并完成对齐调整之后，调用一次 `layout_get_items_properties`，
  传入 `layout_item_ids` 中全部真实 ID 以及完整 `layout_item_ids` 对象。工具会批量取得
  页面尺寸和元素状态，并在当前会话自动保存不可手工重建的布局快照。
- 每次 `layout_add_*`、`layout_move_item`、`layout_resize_item` 后，检查返回中的 `data.rect_mm` 与 `data.auto_adjusted`
- 若 `data.auto_adjusted=true`，继续调用 `layout_move_item` / `layout_resize_item` 回调到目标位置
- 比例尺是例外：不得使用 `layout_resize_item` 强制压缩比例尺；必须使用其实际 `rect_mm`，再通过 `layout_align_items` 相对 `main_map` 对齐
- 导出优先 `export_layout_pdf`。
- 启用质量反馈时，供千问查看的 PNG 按页面规格选用固定审查 DPI：A0=100、A1=140、A2/A3/A4=200，确保单图不超过 1600 万像素；终稿 PNG 必须使用与初版相同的 DPI 和页面尺寸。最终 PDF 仍按布局直接导出，不受审查 PNG 的 DPI 限制。

## 1.点、线、面标准符号化

优先按以下顺序理解图层：

1. 用户明确指定的类型或制图意图。
2. 图层名称，例如“地级城市驻地”“县级城市驻地”“铁路”“公路”“高速公路”“国道”“地区界线”“省界”“河流”“县界”“市界”“省域”。
3. 图层字段，例如 `name`、`type`、`level`、`class`、`admin_level`。
4. 当前图层几何类型、已有渲染器、已有标注和图例表现。

如果图层名已经表达了类别，通常直接映射到标准参数，不要再尝试按字段分类渲染。

点图层:

使用 `layer_set_marker_symbol` 处理城市驻地、县城驻地等点状居民地或行政中心。

参数：

- `layer`：图层名称，必填。
- `layer_type`：`city` 或 `county`，必填。
- `label_field`：标注字段名称，必填，常见为 `name`。

类型选择：

- 地级城市、城市驻地、市级行政中心：使用 `city`。
- 县城、县级驻地、县级行政中心：使用 `county`。

示例：

```yaml
layer: 地级城市驻地
layer_type: city
label_field: name
```

```yaml
layer: 县级城市驻地
layer_type: county
label_field: name
```

线图层：

使用 `layer_set_line_symbol` 处理交通线、行政边界线和线状河流。

参数：

- `layer`：图层名称，必填。
- `line_type`：`铁路`、`高速公路`、`国道`、`省界` 或 `河流`，必填。
- `label_field`：河流名称字段，选填；线状河流需要显示名称时传入，其他线图层不传。

类型选择：

- 铁路、铁路线、轨道交通干线：使用 `铁路`。
- 高速、高速公路、快速路：使用 `高速公路`。
- 当前 `China_map` 的 `公路` 图层：使用 `高速公路` 线型作为普通道路背景的视觉样式；这只是样式复用，不代表数据中的全部要素都是高速公路。
- 国道、G 字头道路：使用 `国道`。
- 地区界线、省界、省级行政边界：使用 `省界`。
- 线状河流、水系线：使用 `河流`。

示例：

```yaml
layer: 铁路
line_type: 铁路
```

```yaml
layer: 公路
line_type: 高速公路
```

```yaml
layer: 国道
line_type: 国道
```

```yaml
layer: 地区界线
line_type: 省界
```

```yaml
layer: 河流线
line_type: 河流
label_field: NAME
```

面图层：
使用 `layer_set_fill_symbol` 处理县、市、省等行政区面，以及面状河流或水域。

参数：

- `layer`：图层名称，必填。
- `layer_type`：`县`、`市`、`省` 或 `河流`，必填。
- `label_field`：标注字段名称，选填；不填写时只设置面符号，不设置或开启标注。

面标注层级原则：

- 面图层标注只设置在“当前制图范围的主体分区层级”上。
- 主体分区层级指构成当前项目目标范围的那一组面单元，也就是读者需要直接识别和比较的主要区域单元。
- 调用 `layer_set_fill_symbol` 时，只有主体分区层级传入 `label_field`；其他面图层只设置符号，不传 `label_field`。
- 如果当前项目是由各个县域拼接成一个市域，则只给 `县` 面图层传 `label_field`。
- 如果当前项目是由各个市域拼接成一个省域，则只给 `市` 面图层传 `label_field`。
- 如果当前项目是由各个省域拼接成一个国域，则只给 `省` 面图层传 `label_field`。
- 如果某个面图层只是背景、外框、底图、水域或辅助范围，通常不传 `label_field`。
- 固定 `China_map` 行军路线图是例外：其中 `省域` 同时承担跨省地理参照，使用已修复的 `NAME` 字段显示省名；不得因它属于底图而省略 `label_field: NAME`。

类型选择：

- 县域、县级行政区、县界面：使用 `县`。
- 市域、地级市行政区、市界面：使用 `市`。
- 省域、省级行政区、省界面：使用 `省`。
- 面状河流、水域面：使用 `河流`。

示例，辅助面或外框只设置面符号：

```yaml
layer: 省域外框
layer_type: 省
```

示例，省域项目由市域拼接，只给市域设置标注：

```yaml
layer: 市域
layer_type: 市
label_field: name
```

示例，国域项目由省域拼接，只给省域设置标注：

```yaml
layer: 省级行政区
layer_type: 省
label_field: name
```

```yaml
layer: 河流面
layer_type: 河流
```

常见映射：

- `地级城市驻地` -> `layer_set_marker_symbol`，`layer_type: city`
- `县城驻地` -> `layer_set_marker_symbol`，`layer_type: county`
- `铁路` -> `layer_set_line_symbol`，`line_type: 铁路`
- `公路` -> `layer_set_line_symbol`，`line_type: 高速公路`（仅复用视觉线型）
- `高速公路` -> `layer_set_line_symbol`，`line_type: 高速公路`
- `国道` -> `layer_set_line_symbol`，`line_type: 国道`
- `地区界线` -> `layer_set_line_symbol`，`line_type: 省界`
- `省界` -> `layer_set_line_symbol`，`line_type: 省界`
- `线状河流` -> `layer_set_line_symbol`，`line_type: 河流`，`label_field: NAME`
- `县级行政区` -> `layer_set_fill_symbol`，`layer_type: 县`
- `地级市行政区` -> `layer_set_fill_symbol`，`layer_type: 市`
- `省域` -> `layer_set_fill_symbol`，`layer_type: 省`，`label_field: NAME`
- `面状河流` / `水域面` -> `layer_set_fill_symbol`，`layer_type: 河流`

当前 `China_map` 数据中，`县级城市驻地` 使用 `label_field: name`；`省域.NAME` 已按 `ADCODE99` 修复为 UTF-8 中文省名，必须使用 `label_field: NAME` 开启省名标注；`河流线.NAME` 是可用的 UTF-8 中文字段，必须通过 `layer_set_line_symbol` 传入 `label_field: NAME` 开启河流沿线标注。

河流名称直接标注在原始 `河流线` 图层上：开启 `mergeLines` 合并同名且首尾相接的短线段，水平放置完整河名；`repeatDistance` 保持 0（不沿线重复），不要开启降级放置，避免出现单字或重复字符。不要为此额外创建新的图层。

### 2. 创建布局

- 调用 `layout_create`创建布局（建议使用 `A1`），把成功结果的布局名称保存为
  `artifacts.layout`。

### 3. 添加主图

- 调用 `layout_add_map` 添加主地图框，将成功结果的 `output` 原样记录为
  `artifacts.layout_item_ids.standard.main_map`。

### 4. 对图层进行路线绘制、点符号化和图文注记
- 调用 `cartography_build_and_draw_from_tables`：
- 参数：
  - `dataset_source`：用户给出叙事文本并完成知识抽取时必须为 `current_session`；只有用户明确提供已有六表目录时才可为 `explicit`。
  - `dataset_dir`：`current_session` 时省略，程序自动使用当前任务的 `intermediate/route_knowledge`；`explicit` 时使用用户明确提供的目录。禁止使用任何写死的旧路线数据目录。
  - `expected_table_counts`：有知识抽取交接结果时，传入其 `counts`。
  - `expected_task_id`：有知识抽取交接结果时，传入任务知识图谱的 `task_id`。
  - `plugin_dir`：<YOUR_HOME>/Library/Application Support/QGIS/QGIS3/profiles/default/python/plugins/RouteBezierPlotPlugin
  - `run_routes`: true
  - `run_symbols`: true
  - `run_callouts`: true
  - `strict`: true

工具成功后必须从 `data.callouts.items[*]` 原样提取 `id` 与
`created_item_ids`，保存到当前制图阶段的 `layout_item_ids.callouts`。
`created_item_ids` 中的 `box`、`image`、`title`、`body` 与
`leader` 不得改名、扁平化或按命名规律重建；`leader` 可能是单个字符串
或字符串数组，必须保持工具返回的原类型与值。只收录 `success=true`
且存在 `created_item_ids` 的项；失败项单独报告，不得为其生成 ID。

`layout_item_ids` 使用固定形状：`standard` 是以语义名为键、真实 ID
为值的对象；`callouts` 是以原 `id` 为键、`created_item_ids` 为值的对象。
例如 `layout_item_ids.callouts.co_sanmenxia.title` 的值是工具返回的
`callout_co_sanmenxia_title`，不是智能体根据名称推测的字符串。

不得调用 `output_get_info`、搜索输出目录或根据已有图层数量来反推
`dataset_dir`。`cartography_build_and_draw_from_tables` 会在修改 QGIS
工程前解析当前会话数据集，并校验六表、编译清单、验证报告、任务编号
和行数；校验失败时必须停止，不得切换其他数据集重试。

### 5. 导出布局流程

按以下顺序执行，不得跳步或换序。

1. 调用 `layout_add_grid`添加网格线（以工程的crs信息为准，间隔不要设置得太密集）。
2. 调用 `layout_add_decorative_border`添加外边装饰框。
3. 调用 `layout_add_label` 添加标题。
4. 调用 `layout_set_label_style` 设置标题样式（不需要边框、背景）。
5. 基于标题相关的主题生成一段制图主题综述(100~150字的中文地理信息描述)，调用`layout_add_label`添加述。
6. 调用 `layout_set_label_style` 设置综述样式（需要边框、白色背景，自行设置字体、字号、颜色等；换行参考宽度根据 `layout_get_item_properties`获取的地图框大小给一个合适的估计值，不宜超过地图框总宽度的1/8）。
7. 调用 `layout_add_legend` 添加图例（关联 `map_id`），再调用 `layout_set_legend_style` 显式传 `include_military_arrow=true`，保留行军箭头图例。
8. 调用 `layout_set_legend_style`设置图例样式（排除军标箭头有关的图层）。
9. 调用 `layout_add_scalebar` 添加比例尺（关联 `map_id`，传 `width=80` 作为条带目标最大宽度，不代表最终元素外框宽度）。
10. 调用 `layout_set_scalebar_style` 设置比例尺样式。
11. 调用 `layout_add_north_arrow` 添加指北针。
12. 调用 `layout_set_north_arrow_style` 设置指北针样式。
13. 使用真实 ID 调用 `layout_get_item_properties` 获取地图框、标题、综述、图例、比例尺、指北针的 `x/y/width/height`，再做对齐与移动。比例尺不得手算横纵坐标，也不得按请求的 80mm 推测宽度；必须调用两次 `layout_align_items`，均传 `item_ids=["scalebar"]`、`reference_item_id="main_map"`，依次使用 `alignment="right"` 和 `alignment="bottom"`。
14. 所有移动和调整完成后，把 `layout_item_ids.standard` 的值以及 `layout_item_ids.callouts` 内所有 `created_item_ids` 展平为字符串 ID 数组，调用一次 `layout_get_items_properties`；同时把完整 `layout_item_ids` 对象传给该工具。工具必须成功返回 `data.layout_snapshot_path`，该快照已经包含页面尺寸、完整 ID 映射和每个真实 ID 的状态。把路径原样保存为 `artifacts.review.layout_snapshot_path`；禁止再把工具 `data` 手工摘抄成布局状态摘要。
15. 用户启用质量反馈时，按 A0=100、A1=140、A2/A3/A4=200 选择审查 DPI，调用 `layout_export_image`，传当前布局、选定的 `dpi`、`format=png` 和独立“初版”文件名。把成功结果的 `output` 原样保存为 `artifacts.review.source_initial_image_path`；DPI 由质量工具直接读取图片并结合布局快照核对，不再作为跨智能体参数交接。未启用质量反馈时跳过本步。
16. 调用 `export_layout_pdf` 导出 PDF；不传 `output` 时默认写入 AutoCarto 当前会话 `exports` 子目录，若需要指定文件名，应根据当前任务标题生成，例如 `output: 行军路线图.pdf`，不得沿用旧河南底图名称。

每个 `layout_add_*` 成功后立即把结果的 `output` 保存到
`artifacts.layout_item_ids.standard`：主图、装饰边框、标题、综述、图例、比例尺和指北针
分别使用键 `main_map`、`decorative_border`、`title`、`overview`、`legend`、
`scalebar`、`north_arrow`。只记录成功返回的真实 `output`，禁止猜测。

启用质量反馈时，初版跨智能体交接只返回
`artifacts.review.source_initial_image_path`、`artifacts.review.layout_snapshot_path`、
原始任务和锁定内容；不得展开或重写完整 `layout_item_ids` / `layout_item_state`。
质量反馈工具会从快照读取真实 ID 与状态。若快照生成失败，本阶段必须原样报告，
不能猜测名称、页面坐标或 DPI。

## 字号规则（单位 mm）

按纸张规格选择字号，禁止使用 pt。

| 元素         |  A0 |   A1 |  A2 |  A3 |  A4 |
| ------------ | --: | ---: | --: | --: | --: |
| 主标题       |  15 | 11.5 |   9 |   8 | 5.5 |
| 图例标题     |   4 |  3.5 |   3 | 2.5 | 2.5 |
| 图例正文     |   3 |  2.5 | 2.5 |   2 |   2 |
| 比例尺       | 2.5 |    2 |   2 |   2 |   2 |
| 右上综述标题 |   4 |  3.5 |   3 | 2.5 | 2.5 |
| 右上综述正文 |   3 |  2.5 | 2.5 |   2 |   2 |

最小映射要求：

- 标题字号使用“主标题”。
- 综述正文字号使用“右上综述正文”。
- 图例标题字号与“右上综述标题”一致。
- 图例正文使用“图例正文”。
- 比例尺字号使用“比例尺”。

## 对齐与定位规则

以地图框和外边装饰框为锚点。先通过 `layout_get_item_properties` 读取外边装饰框属性（以 `outer_rect_mm`为准），设：

- `bx = border.x`
- `by = border.y`
- `bw = border.width`
- `bh = border.height`

设地图框属性为：

- `mx = map.x`
- `my = map.y`
- `mw = map.width`
- `mh = map.height`

设标题、综述、图例、比例尺、指北针尺寸分别为：

- 标题 `tw/th`
- 综述 `ow/oh`
- 图例 `lw/lh`
- 比例尺 `sw/sh`
- 指北针 `nw/nh`

目标位置（左上角）：

- 标题在外边装饰框外部正上方居中：`title_x = bx + (bw - tw) / 2`，`title_y = by - th`
- 综述在地图框内部右上角：`overview_x = mx + mw - ow`，`overview_y = my`
- 图例在地图框内部左下角：`legend_x = mx`，`legend_y = my + mh - lh`
- 比例尺在地图框内部右下角：由 `layout_align_items` 读取真实外框尺寸并相对 `main_map` 执行 `right`、`bottom` 对齐；公式 `mx + mw - sw`、`my + mh - sh` 只用于结果检查，不由智能体手算
- 指北针在地图框内部左上角：`north_x = mx`，`north_y = my`

标题、综述、图例和指北针按上式调用 `layout_move_item`；比例尺使用上述参考元素对齐工具。

## 质量反馈修改约束

- 质量反馈的 `suggestions` 是候选建议，不是强制命令；本节和上述字号、定位规则
  始终优先。执行前必须核对建议中的图片证据是否真实、`target_item_ids` 是否来自
  布局快照、工具名与参数是否存在。
- 标题上外居中、综述右上内、图例左下内、比例尺右下内、指北针左上内属于本图种
  的合法锚点。元素位于 `main_map` 内或矩形与 `main_map` 相交本身不构成压盖。
- 执行任何 `move/resize` 前，使用当前 `rect_mm` 计算修改后的
  `left/top/right/bottom`；整个矩形必须留在对应合法容器内，不得超出主图、装饰边框
  或页面，也不得与其他固定元素相交。否则将该建议记录为 `skipped`。
- 所有字号仍以 mm 为单位。若先修改标签样式，样式工具可能改变宽高，必须重新读取
  `rect_mm`，再按本 skill 的锚点公式计算位置；禁止沿用质量建议中的旧尺寸坐标。
- 本 skill 的纸张字号表是质量修改阶段的锁定规则。cartographer 必须拒绝任何改变
  `font_size` 的质量建议，也不得仅放大标题、综述或图例外框来制造空白。文字完整且
  没有裁切时，保持初版字号与自动适配的文本框尺寸不变。
- 图文标注是 `box/image/title/body/leader` 的组合。整组调整只能调用
  `layout_move_callout`，并把 `layout_item_ids.callouts` 中该组原样的
  `created_item_ids` 作为 `callout_item_ids` 传入；`x/y` 是标注框目标左上角绝对毫米
  坐标，`container_item_id` 使用 `layout_item_ids.standard.main_map`。工具负责统一移动
  边框、图片、标题和正文，固定地图锚点并重新计算牵引线。调用后必须检查
  `data.new_box_rect`、`data.anchor_preserved` 和每条牵引线的新旧长度。
  不得分别移动子元素、猜测牵引线坐标、伪造 ID 或用纯文本框替代。
- 图文标注可以覆盖普通行政区面、普通行政边界、一般道路、一般河流、低优先级县级地名和
  其他 background 要素。这些低优先级底图不是必须避让的内容；不得为了寻找纯空白区域，把标注框移到
  行政区主体之外、地图内容稀疏的边缘或明显远离锚点的位置。移动建议只需避让行军路线、箭头、
  已专题符号化的节点及其直接关联的重要地名、其他图文标注和固定布局元素，并优先选择距原位置最近的可用位置。
- 当 quality_feedback 已为 `layout_move_callout` 提供 `executable=true`、完整
  `callout_item_ids` 和目标 `x/y` 时，cartographer 只负责核对目标矩形是否位于
  `main_map`、是否与已知布局元素相交以及参数是否属于工具契约；不得以“看不到初版
  图片”或“没有整组移动工具”为由跳过。几何检查通过后调用工具，但若建议明显把标注框推到行政区主体之外、稀疏边缘或过度远离锚点，则记为 `skipped`；工具失败则原样报告。


## 输出检查清单

- 图层几何类型与工具匹配：点用 marker，线用 line，面用 fill。
- 点图层已提供 `label_field`，面图层仅在需要标注时提供 `label_field`。
- 线图层的 `line_type` 使用标准枚举值；线状河流已传入 `label_field: NAME`。
- 面图层的 `layer_type` 使用标准中文枚举值。
- 不确定图层语义时，先根据图层名和字段推断；仍不确定时再向用户确认。
- [ ] 已调用 `cartography_build_and_draw_from_tables`，且路线、点符号化和图文注记正确应用到目标图层
- [ ] 已把 `data.callouts.items[*].created_item_ids` 原样写入当前阶段的 `layout_item_ids.callouts`
- [ ] 已把主图、边框、标题、综述、图例、比例尺和指北针的真实 `output` 写入当前阶段的 `layout_item_ids.standard`
- [ ] 已将完整 `layout_item_ids` 传给 `layout_get_items_properties`，并原样交接其返回的 `data.layout_snapshot_path`
- [ ] 比例尺未经过 `layout_resize_item` 强制缩放，且已使用实际尺寸相对 `main_map` 完成右、下对齐
- 已使用 `get_layers_info` 生成综述。
- 所有字号均按对应纸张列取值且单位为 mm。
- 元素位置满足：标题上外中、综述右上内、图例左下内、比例尺右下内、指北针左上内。
