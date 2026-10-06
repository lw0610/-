---
name: print-layout-thematic
description: 区域交通线路图布局制作，按主图四角进行刚性对齐，输出包含主图、标题、图例、数据综述、比例尺、指北针的可交付专题图
kind: workflow
---

# 区域交通线路图布局制作流程（四角对齐版）

## 目标

本流程用于制作区域交通线路图，并强制以主图边界为锚点进行要素对齐：

- 图例左下角与主图左下角对齐
- 综述右上角与主图右上角对齐
- 指北针左上角与主图左上角对齐
- 比例尺宽度固定为 80mm，且右下角与主图右下角对齐
- 装饰边框包裹主图边界，外框相对主图四边各外扩 5mm，总宽高分别比主图多 10mm；不得改变主图的位置、尺寸、比例尺或范围
- 标题位于装饰图框上方，按图框宽度水平居中，字体加粗

## 前置条件

- 已有交通相关图层（道路/铁路/站点/行政区界等）
- 已确定输出纸张（推荐 A4 横向）

## 严格执行约束（必须）

- 不得省略：主图、标题、图例、数据综述、比例尺、指北针
- 必须调用 `layout_add_decorative_border` 为主图生成装饰边框；边框只能由 `main_map` 的四角 bounds 向外扩展计算，不能作为新的对齐基准。
- 每次 `layout_add_*`、`layout_move_item`、`layout_resize_item` 后，检查返回中的 `data.rect_mm` 与 `data.auto_adjusted`
- 若 `data.auto_adjusted=true`，继续调用 `layout_move_item` / `layout_resize_item` 回调到目标位置
- 导出优先 `export_layout_pdf`

## 工作流步骤

### 1. 创建布局

- 调用 `layout_create`
- 调用 `layout_set_page_size`（推荐 A4 横向 297x210mm）

### 2. 添加主图

- 调用 `layout_add_map`，`item_id=main_map`
- 建议 `allow_auto_adjust=false`
- 若不传 `x/y/width/height`，使用默认版式（A4 横向模板：x=12, y=18, width=268, height=170）
- 调用 `layout_set_map_extent`（建议 `preserve_frame_size=true`）或 `layout_set_map_scale`

### 3. 获取主图四角（关键步骤）

- 调用 `layout_get_item_corners`，参数：`layout`、`item_id=main_map`
- 从返回中读取：
  - `bounds_mm.x_min`
  - `bounds_mm.y_min`
  - `bounds_mm.x_max`
  - `bounds_mm.y_max`

### 4. 添加主图装饰边框（外扩包裹主图，不改变主图）

- 调用 `layout_add_decorative_border`，参数必须来自第 3 步读取的 `main_map` bounds，并以主图中心不变的方式四边各外扩 5mm：
  - `layout = <当前布局名>`
  - `item_id = main_map_decorative_border`
  - `x = map_x_min - 5`
  - `y = map_y_min - 5`
  - `width = (map_x_max - map_x_min) + 10`
  - `height = (map_y_max - map_y_min) + 10`
  - `rotation = 0`
- 边框只是视觉装饰，禁止在第 3 步之后通过移动、缩放 `main_map` 来迁就边框；若页面四周不足 5mm，必须回到第 2 步先把 `main_map` 向内移动或缩小，重新获取四角后再计算边框，不能让边框越出页面或压住其他布局元素。
- 若边框返回 `data.auto_adjusted=true`，只允许移动/缩放边框本身回到上述外扩 bounds；不得调整 `main_map`，不得让边框与图例、综述、指北针或比例尺产生遮挡。
- 边框应在主图之后、标题/图例/综述/指北针/比例尺之前添加，使其位于主图上方但不遮挡后续布局元素；必要时用 `layout_set_item_z_order` 将其置于 `main_map` 上方、其他说明元素下方。

### 5. 添加标题（图框上方、加粗居中）

- 调用 `layout_add_label` 添加标题，参数必须基于第 4 步计算的装饰图框 bounds：
  - `layout = <当前布局名>`
  - `item_id = layout_title`
  - `text = <专题图标题>`（未指定时使用“区域交通线路图”）
  - `x = border_x`
  - `y = border_y - title_gap - title_h`
  - `width = border_w`
  - `height = title_h`
  - `font_size = 18`
- 固定建议：`title_h = 10`，`title_gap = 2`，即标题底边与图框顶边至少保留 2mm 间距。
- 调用 `layout_set_label_style`：
  - `item_id = layout_title`
  - `font_bold = true`
  - `h_align = center`
  - `v_align = middle`
  - `margin = 0`
- 若 `border_y - title_gap - title_h < 0` 或标题与图框发生重叠，必须回到第 2 步将 `main_map` 整体下移或缩小，重新获取主图四角并重算边框和标题；不得为了标题单独移动图框或让标题压住图框。

### 6. 添加并对齐图例（左下对齐）

- 调用 `layout_add_legend`（建议：`allow_auto_adjust=false`、`resize_to_contents=false`、`frame_color=#9DC3E6`、`frame_width=0.6`）
- 调用 `layout_get_item_properties` 获取图例实际 `legend_h`
- 调用 `layout_move_item`：
  - `x = map_x_min`
  - `y = map_y_max - legend_h`

### 7. 添加并对齐数据综述（右上对齐）

- 调用 `layout_add_file_overview`
- 调用 `layout_get_item_properties` 获取综述实际 `overview_w`
- 调用 `layout_move_item`：
  - `x = map_x_max - overview_w`
  - `y = map_y_min`

### 8. 添加并对齐指北针（左上对齐）

- 调用 `layout_add_north_arrow`
- 调用 `layout_move_item`：
  - `x = map_x_min`
  - `y = map_y_min`

### 9. 添加并对齐比例尺（右下对齐，固定宽度）

- 调用 `layout_add_scalebar`，显式传 `width=80`
- 再调用 `layout_resize_item` 强制 `width=80`（防止样式导致宽度漂移）
- 调用 `layout_get_item_properties` 获取比例尺实际 `scalebar_h`
- 调用 `layout_move_item`：
  - `x = map_x_max - 20`
  - `y = map_y_max - scalebar_h`

### 10. 导出

- 调用 `layout_info` 做完整性检查
- 调用 `export_layout_pdf`

## 角点对齐计算公式（mm）

- 主图左上角：`(map_x_min, map_y_min)`
- 主图右上角：`(map_x_max, map_y_min)`
- 主图左下角：`(map_x_min, map_y_max)`
- 主图右下角：`(map_x_max, map_y_max)`

- 图例左下对齐：`legend_x = map_x_min`，`legend_y = map_y_max - legend_h`
- 综述右上对齐：`overview_x = map_x_max - overview_w`，`overview_y = map_y_min`
- 指北针左上对齐：`north_x = map_x_min`，`north_y = map_y_min`
- 比例尺右下对齐：`scalebar_x = map_x_max - 20`，`scalebar_y = map_y_max - scalebar_h`
- 装饰边框外扩包裹主图：`border_x = map_x_min - 5`，`border_y = map_y_min - 5`，`border_w = map_x_max - map_x_min + 10`，`border_h = map_y_max - map_y_min + 10`
- 标题图框上方居中：`title_x = border_x`，`title_y = border_y - title_gap - title_h`，`title_w = border_w`，`title_h = 10`，`title_gap = 2`

## 输出检查清单

- [ ] 已成功调用 `layout_get_item_corners` 获取主图边界
- [ ] 已调用 `layout_add_decorative_border`，且边框外框相对 `main_map` 四边各外扩 5mm，宽高分别比主图多 10mm
- [ ] 边框没有越出页面，没有移动主图，也没有遮挡图例、综述、指北针或比例尺
- [ ] 已调用 `layout_add_label` 和 `layout_set_label_style` 添加标题，标题位于图框上方、加粗、按图框宽度居中，且没有压住图框
- [ ] 图例左下角与主图左下角对齐
- [ ] 综述右上角与主图右上角对齐
- [ ] 指北针左上角与主图左上角对齐
- [ ] 比例尺宽度固定 20mm 且右下角与主图右下角对齐
- [ ] 已导出 PDF 并返回输出路径
