---
name: vector-selection
description: 属性/空间选择、提取与交互式选择工具集合
kind: category
enabled: true
tags:
- vector
- selection
tools:
- selection_by_expression
- selection_by_attribute
- selection_clear
- extract_by_expression
- extract_selected_features
- extract_by_attribute
- random_selection
- extract_by_location
- random_extract
display_name: 要素筛选
domain: 矢量
system: false
hidden: false
auto_mount: ''
---

# 矢量选择工具集

本 package 收纳 属性/空间选择、提取与交互式选择工具集合，用于在运行时构建统一的扁平工具索引。

显示和叠加使用 QGIS 动态投影；只有分析、量算明确需要时才转换。先核实源 CRS、坐标单位和算法处理方式，不能因显示 CRS 不同而批量重投影；定义 CRS 只改元数据，重投影才改变坐标。转换时生成独立结果，核对范围、位置和精度。

这些工具用于相应数据的真实处理。先核实输入图层、字段、几何或波段、参数单位及输出位置；不要凭示例推断已有数据。执行后检查实际返回状态、真实产物、范围、类型及数量，空值和无效结果如实报告，保留原始数据和可追溯的处理参数。需要更细的方法、参数选择、常见错误和检查清单时，按任务读取下面的参考资料，不必把所有方法逐项执行。

## 参考资料

使用 reference_read 传入“vector-selection/references/文件.md”读取：

- 条件提取：references/条件提取.md
- 随机抽样：references/随机抽样.md
