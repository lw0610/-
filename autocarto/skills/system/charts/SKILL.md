---
name: charts
description: 根据图层属性与栅格数值生成统计图表。
kind: category
runtime: qgis
enabled: true
tools:
- mean_and_standard_deviation_plot
- bar_chart
- scatter_plot
- polar_plot
- vector_layer_histogram
- box_plot
- raster_layer_histogram
- scatter_3d_plot
display_name: 统计图表
domain: 制图
system: false
hidden: false
auto_mount: ''
---

# 统计图表使用说明

先核实已加载的数据、字段类型、空值和分组含义。分类比较用 bar_chart，变量关系用 scatter_plot 或 scatter_3d_plot，分布用 vector_layer_histogram/raster_layer_histogram 和 box_plot，方向数据用 polar_plot，均值与离散程度用 mean_and_standard_deviation_plot。图表类型应匹配分析目标，不仅按外观选择。

参数引用真实字段，写清轴标签、单位、分组和样本过滤规则。直方图箱数随数据量和分布调整，类别过多可依据任务规则汇总，不能随意删掉不利结果。空值不等于零，异常值需核实来源并披露处理方式。

检查输出文件、样本数、图表轴、范围和标签。bar_chart 返回现成 svg_path/picture_path 时，直接交 layout_add_picture；不要再写脚本把 HTML 转图片。嵌入后检查清晰度、宽高比和页面遮挡，记录真实产物。
