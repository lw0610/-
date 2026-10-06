---
name: print-layout-standard
description: 导出布局固定流程。用于用户要求按标准步骤生成布局图时，严格执行创建布局、添加地图、读取图层信息、添加并样式化综述/标题/图例/比例尺/指北针，并在最后基于元素属性完成对齐定位。
---
# 导出布局流程

按以下顺序执行，不得跳步或换序。

1. 调用 `layout_create` 创建布局（建议优先使用 `A1`）。
2. 调用 `layout_add_map` 添加主地图框，记录 `map_id`。
3. 调用 `layout_add_decorative_border`添加外边装饰框。
4. 调用 `get_layers_info` 获取全部图层信息。
5. 基于图层信息生成一段综述文本（一段简要的中文地理信息描述），调用 `layout_add_label` 添加综述（`auto_wrap=true`，宽度根据 `layout_get_item_properties`获取的地图框大小给一个合适的估计值，不宜超过地图框总宽度的1/6）。
6. 调用 `layout_set_label_style` 设置综述样式（需要边框、白色背景，自行设置字体、字号、颜色等）。
7. 调用 `layout_add_label` 添加标题（`auto_wrap=false`）。
8. 调用 `layout_set_label_style` 设置标题样式（不需要边框、背景）。
9. 调用 `layout_add_legend` 添加图例（关联 `map_id`）。
10. 调用 `layout_add_scalebar` 添加比例尺（关联 `map_id`）。
11. 调用 `layout_set_scalebar_style` 设置比例尺样式。
12. 调用 `layout_add_north_arrow` 添加指北针。
13. 调用 `layout_set_north_arrow_style` 设置指北针样式。
14. 调用 `layout_get_item_properties` 获取地图框、标题、综述、图例、比例尺、指北针的 `x/y/width/height`，再做对齐与移动。

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
- 比例尺在地图框内部右下角：`scalebar_x = mx + mw - sw`，`scalebar_y = my + mh - sh`
- 指北针在地图框内部左上角：`north_x = mx`，`north_y = my`

按上式调用 `layout_move_item` 完成位置调整。

## 结果检查

- 已严格按 1-14 步执行。
- 已使用 `get_layers_info` 生成综述。
- 所有字号均按对应纸张列取值且单位为 mm。
- 元素位置满足：标题上外中、综述右上内、图例左下内、比例尺右下内、指北针左上内。