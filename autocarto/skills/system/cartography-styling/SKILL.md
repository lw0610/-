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

单层套用使用 layer_load_style（style 为样式名称），多层使用 layer_load_styles，items 每项为 layer、style、可选 label_field。字段采用数据的真实名称，不为匹配样式改动用户数据。检查逐项返回的 label_field 与 warnings。保存样式使用 layer_save_style；资源库“从当前工程保存”可把多个图层分别保存为单个样式。

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
