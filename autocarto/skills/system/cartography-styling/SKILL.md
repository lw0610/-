---
name: cartography-styling
description: 地图符号化相关工具集合
kind: category
runtime: qgis
enabled: true
tags:
- cartography
- styling
tools:
- layer_set_symbology
- layer_load_styles
- cartography_set_style
- cartography_graduated_style
- cartography_add_labels
- layer_get_style_info
- cartography_25d_style
- cartography_heatmap_style
- cartography_point_displacement
- cartography_rule_based_style
- cartography_symbolize_points_by_mapping
- layer_load_style
- layer_save_style
- layer_set_blend_mode
- layer_set_color_ramp
- layer_set_opacity
- layer_set_symbol_size_scale
display_name: 符号与标注
domain: 制图
system: false
hidden: false
auto_mount: ''
---

# 符号与标注使用说明

用户或流程明确了颜色、线宽、线型和标注时，用 `layer_set_symbology` 翻译为参数执行；只描述表达效果时，结合几何类型、地图范围和纸张选取参数。复杂标准符号按选定样式加载；没有规定画法时可用图层登记的默认样式或简洁表达。不要把已经分级的专题层再次改成统一填充。

## 按流程参数设置底图

`layer_set_symbology` 接收 `items` 和统一 `scale`。每项含 `layer`、`symbol:{type,layers}`、`labels`：
- `type` 为 `fill`、`line`、`marker`，符号层按自下而上列出。面写填充、边线；线写颜色、宽度、线型、虚线、偏移、端点；点写形状、大小、填充和描边。公路的黑边灰芯用两层线，驻地的同心空心圆用两层点。
- `labels:null` 明确关闭标注。需要标注时写 `field:auto`、`font:auto`，由图层登记的标注字段和实际字段确定名称，字体使用可用中文黑体。写明 `size`、`unit:mm|pt`、颜色和 `placement:horizontal|line|around_point`；需要合并相接河段并避免重复标注时设置 `merge_lines:true` 和 `repeat:0`，明确要求所有要素显示标注时才用 `display_all:true`。文字描边用 `buffer:{size,color}`（单位毫米），无描边则省略或传 `null`。`bold` 控制粗体（默认 `false`）；`obstacle` 控制该图层要素是否参与其他标注避让（默认 `true`），按本次表达需要设置。
- 流程以 A1 为基准时，`scale` 为 A1=1、A0=1.41、A2=0.71、A3=0.5、A4=0.35；不要先自行缩小参数后再传同一缩放，避免缩两次。字号统一换算后不小于 1.8 毫米，线宽不小于 0.1 毫米。

返回 `data.items` 中 `request` 是输入，`requested` 是缩放与自动字段/字体解析后的期望，`actual` 是 QGIS 读回值；逐项看 `warnings` 与 `differences`。颜色应一致，线宽允许 0.01 毫米误差，标注字段、开关和放置方式应一致；不一致依据具体差异在原步骤修正，不重新取数据。字段不存在或字体回退如实说明。

单层套样式使用 `layer_load_style`（`style` 为资源名），多层用 `layer_load_styles`，每项为 `layer`、`style`、可选 `label_field`，单层和批量调用的顶层 `scale` 使用同一纸张比例。按用户或流程选择的样式应用到对应图层，检查真实字段、字体和警告，依本次纸张处理尺寸。不要为匹配样式改写原始数据。保存外观使用 `layer_save_style`；资源库可以逐层保存。

## 1. 确认制图数据

使用 `get_layers_info` 或 `get_active_layer_info` 核对目标图层、几何类型、目标字段、数值类型和数据来源。名称相近的边界、统计关联结果和验证副本可能同时存在，选择已核验且含专题字段的一个主图层。

## 2. 设置分级设色

连续数值字段调用 `cartography_graduated_style`：

- `layer`：已确认的主图层名称。
- `field`：专题数值字段。
- `classes`：请求分级数量，通常 5 级，允许 2-20；实际分级数受不同有效值数量和方法影响。
- `method`：`jenks`（自然断点）、`quantile`（分位数）、`equal_interval`（等间距）或 `stddev`（标准差）。根据分布与表达目的选择；省略时工具使用 `quantile`，不得将它报告为自然断点。
- `color_ramp`：QGIS 样式库或 ColorBrewer 色带名称，连续数量可用 `YlOrRd`、`Blues` 等顺序色带；省略时使用 `YlOrRd`。

工具拒绝不存在或非数值字段、少于两个不同有限值、无效分类方法、未知或单色颜色带。空值和非有限值不参与分类，不等于零；将返回的数量与警告纳入数据说明。

QML 样式用于复用底图符号，不能替代按字段分级，且会覆盖已有渲染器。分类、规则、热力图等其他表达方式需要其对应的真实工具或专门脚本能力。

`cartography_rule_based_style` 的每项规则为 `label`、`expression`、`color`，线图层可加 `width`（毫米）；顶层 `scale` 按纸张一次缩放，例如 A1 的 0.8 毫米线宽用于 A3 时传 `width:0.8, scale:0.5`，返回 `data.rules[].width_mm` 为实际值。表达式需精确区分真实类别，不能用“1号”模糊匹配同时套到“11号”。未匹配任何规则的要素不会绘制；需要保留未归类要素时显式提供 ELSE 规则，不依据缺失名称编造类别。

## 3. 标注与结果核验

需要地名标注时，调用 `cartography_add_labels`，传 `layer`、`field`，可传 `font_size`（pt）、`color`；此工具不会替换分级渲染器。不要为添加标注再次调用统一填充工具。

随后调用 `layer_get_style_info`，检查真实返回：

- `renderer_type` 为 `graduatedSymbol`，`renderer_field` 与任务字段一致。
- `classification_method` 与选用方法一致。
- `class_count` 至少为 2，`distinct_color_count` 至少为 2。
- `ranges` 中上下界、图例标签和颜色有效，覆盖目标数据的有限值范围。

只有这些检查通过才交接排版。工具失败或能力缺失时返回具体阻塞项；不把已导出文件当作专题表达完成的依据。

## 4. 输出已核验的样式状态

向布局流程交接已核验的主专题图层、必要辅助图层、真实分级结果、指标与单位、年份和来源。地图框建立后使用 `layout_set_map_layers` 锁定本次制图图层，避免验证副本、其他任务影像或辅助表进入地图及图例；用 `layout_set_map_extent` 设置对应研究区范围，范围坐标须与地图框 CRS 一致，不能直接把经纬度填入投影坐标参数。布局图例应显示已验证的分级区间及单位。
