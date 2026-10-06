---
name: print-layout-statistical
description: 行政区统计专题图版面，核验原生分级设色与图例一致性、缺测表达、研究区范围和来源说明后导出
kind: workflow
requires_tools:
- cartography_graduated_style
- cartography_add_labels
- cartography_set_style
- layer_get_style_info
- layout_create
- layout_add_map
- layout_set_map_layers
- layout_set_map_extent
- layout_move_item
- layout_resize_item
- layout_add_label
- layout_set_label_style
- layout_add_scalebar
- layout_add_north_arrow
- layout_get_item_properties
- layout_add_legend
- layout_set_legend_style
- layout_info
- export_print_layout
- layout_export_svg
---

# 统计专题图符号与版面

供 `cartographer` 使用。前置条件是数据角色已经核验行政区数值图层（新派生或已有图层），交接了真实图层名称、数值字段、指标、年份、单位、来源和缺测报告。不要在这里重新关联或修改统计数据，不调用行军路线绘制，不加入军标箭头、柱状图或未经支持的比例符号。

## 1. 原生分级设色与标注

先读取 `thematic-map-styling`，将交接的数值字段传给 `cartography_graduated_style`。按数据分布和任务选择真实支持的 `jenks`、`quantile`、`equal_interval`、`stddev`，请求级数通常为 5，范围为 2–20；不向工具发送自定义断点等未声明参数。默认是分位数，不得报告为自然断点。

连续数量采用有次序的多色阶颜色带，例如 `YlOrRd` 或 `Blues`。需要地区名称时，用 `cartography_add_labels` 指定 `stat_region` 或交接确认的名称字段。随后读取 `layer_get_style_info`，确认 renderer_type=graduatedSymbol、renderer_field、classification_method、class_count、distinct_color_count、ranges 及每级上下界/颜色/标签。只有一个不同有限值时不能声称完成分级。

NULL 不能当零，零仍参加分类。若分级渲染器未显示 NULL 地区，使用数据角色已经加载并交接的独立灰底图层，调用 `cartography_set_style(layer=<灰底>,style_type=simple,color=#e5e7eb)`，置于专题层下方。两层应使用同一套已核验的行政区几何，但必须是独立图层对象；新 CSV 分支使用同一派生面文件。灰底缺失时退回数据角色补载；制图角色没有 `data_load_vector`，不得假造调用。统一填充仅作用于灰底，不能对专题面调用它或 `layer_set_fill_symbol` 覆盖分级渲染器。地图锁定这两层，图例只列专题层，另加“灰色区域：无有效统计值”的说明，并根据报告区分缺测、未匹配和排除数量，不混称零值。

## 2. 建立版面与锁定范围

根据研究区形状选择纸张；未指定可从 A4 横向（297×210 mm）开始。调用 `layout_create` 的 `name/page_width/page_height/units` 创建独立布局，不覆盖其他任务布局。`layout_add_map` 建立主图；位置与尺寸用 `layout_move_item`、`layout_resize_item` 调整，避免向添加工具传入未声明参数。

- 用 `layout_set_map_layers(layout,item_id,layers)` 锁定专题层与明确需要的底图，专题层在上。不要带入验证副本、CSV 表、其他任务图层。
- 优先用 `layout_set_map_extent` 的 `layer=<实际专题图层ID或唯一名称>`，由工具将图层范围转到地图框 CRS；`padding` 为比例，默认 0.03，保留 `preserve_frame_size=true`。确认返回的 extent/crs 覆盖研究区。不把经纬度直接当投影坐标，不用整个工程画布范围替代目标范围。
- 设置主图上方标题、图侧图例与下方信息栏，预留合理留白；不沿用行军路线的“四角强制对齐”或装饰军标。创建后读取真实元素矩形，检查完整边界及相互遮挡。

## 3. 原生图例与统计口径

用 `layout_add_legend` 关联 `map_id`；再调用 `layout_set_legend_style`，显式 `include_military_arrow=false`、`legend_layers=[<专题图层>]` 限定图例，`title` 写成真实指标与单位。原生图例应来自实际分级渲染器，不手工绘制色块或另编区间。

标题反映研究区、年份和指标；用 `layout_add_label` 与 `layout_set_label_style` 添加年份、单位、数据来源、实际分类方法及缺测说明。资料未提供来源时明确“来源未提供”，不生成虚假权威来源或数值综述。按任务添加与主图关联的比例尺和指北针；使用真实返回的尺寸完成对齐，不凭缩放外框改变比例尺语义。

## 4. 核验后导出

- 再读取 `layer_get_style_info`，确认标注及布局操作没有把专题渲染器变成 singleSymbol。
- 通过 `layout_get_item_properties` / `layout_info` 核对地图锁定图层、extent/crs、原生图例的真实图层和条目，以及标题、来源等必要元素；核对分级标签、级数、颜色和数值单位一致，不只检查元素是否存在。
- 检查页面边界、文字裁切、图例溢出、主图遮挡和未说明的缺测；修正后再次读取实际状态。
- 使用 `export_print_layout` 导出默认 PNG 成品；用户明确需要 SVG 时可用 `layout_export_svg`。需要 PDF 时可调用 `export_layout_pdf`。返回工具确认的路径、布局名、元素 ID、实际分类结果和检查结论，不把成功导出当作数据或表达正确的替代证据。

## 5. 工具故障处理

布局工具返回内部异常（如 `ImportError` 或 `handler 执行失败`）时，不得据此认定当前工程损坏，也不得擅自调用 `close_instance`、`open_project`、`create_project` 关闭、重开或重置工程。保留已生成数据、已加载图层及已创建布局。

仅当原始错误明确指出可修正的参数或前置条件时，才针对该原因修正并重试一次；无明确依据或重试仍失败时停止该阶段，不重复同参数、不只更换 `item_id` 或布局名碰运气。`cartographer` 向编排返回工具名、实际参数、原始错误及已完成产物；`orchestrator` 保留原始错误并报告阻塞，不把未验证的故障推测改写成恢复指令。
