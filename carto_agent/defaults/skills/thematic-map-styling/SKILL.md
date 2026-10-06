---
name: thematic-map-styling
description: 基于数值字段制作分级设色专题图，设置真实分类方法与颜色带、标注，并核验渲染结果后交接布局
kind: workflow
requires_tools:
- cartography_graduated_style
- cartography_add_labels
- layer_get_style_info
---

# 专题地图符号化

本流程用于人口、密度、面积、温度等数值主题的矢量专题表达。行政区人口总量与人口密度含义不同，应沿用任务和数据交接中明确的指标、统计年份、空间单元与单位；没有面积和量算依据时不能把总量改称密度。

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

`layer_set_fill_symbol` 是行政区底图的统一填充工具，不能替代按字段分级，且会覆盖已有分级渲染器。分类、规则、热力图等其他表达方式需要其对应的真实工具或专门脚本能力，不能用统一填充假装完成。

## 3. 标注与结果核验

需要地名标注时，调用 `cartography_add_labels`，传 `layer`、`field`，可传 `font_size`（pt）、`color`；此工具不会替换分级渲染器。不要为添加标注再次调用统一填充工具。

随后调用 `layer_get_style_info`，检查真实返回：

- `renderer_type` 为 `graduatedSymbol`，`renderer_field` 与任务字段一致。
- `classification_method` 与选用方法一致。
- `class_count` 至少为 2，`distinct_color_count` 至少为 2。
- `ranges` 中上下界、图例标签和颜色有效，覆盖目标数据的有限值范围。

只有这些检查通过才交接排版。工具失败或能力缺失时返回具体阻塞项，由编排智能体安排对应修复或脚本能力；不把已导出文件当作专题表达完成的依据。

## 4. 交接布局

向布局流程交接已核验的主专题图层、必要辅助图层、真实分级结果、指标与单位、年份和来源。地图框建立后使用 `layout_set_map_layers` 锁定本次制图图层，避免验证副本、其他任务影像或辅助表进入地图及图例；用 `layout_set_map_extent` 设置对应研究区范围，范围坐标须与地图框 CRS 一致，不能直接把经纬度填入投影坐标参数。布局图例应显示已验证的分级区间及单位。
