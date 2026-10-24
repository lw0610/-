---
name: vector-fields
description: 字段、属性表、连接与属性重构工具集合
kind: category
runtime: qgis
enabled: true
tools:
- add_autoincremental_field
- add_field_to_attributes_table
- add_unique_value_index_field
- add_xy_fields
- advanced_python_field_calculator
- create_attribute_index
- delete_column
- delete_duplicates_by_attribute
- drop_geometries
- explode_hstore_field
- extract_binary
- field_calculator
- join_attributes_by_field_value
- join_attributes_by_location
- join_attributes_by_location_summary
- join_attributes_by_nearest
- order_by_expression
- refactor_fields
- rename_table_field
- retain_fields
- split_features_by_character
- text_to_float
tags:
- vector
- fields
- attributes
display_name: 属性字段
domain: 矢量
system: false
hidden: false
auto_mount: ''
---

# 矢量字段与属性工具集

本 package 收纳 字段、属性表、连接与属性重构工具集合，用于在运行时构建统一的扁平工具索引。

显示和叠加使用 QGIS 动态投影；只有分析、量算明确需要时才转换。先核实源 CRS、坐标单位和算法处理方式，不能因显示 CRS 不同而批量重投影；定义 CRS 只改元数据，重投影才改变坐标。转换时生成独立结果，核对范围、位置和精度。

这些工具用于相应数据的真实处理。先核实输入图层、字段、几何或波段、参数单位及输出位置；不要凭示例推断已有数据。执行后检查实际返回状态、真实产物、范围、类型及数量，空值和无效结果如实报告，保留原始数据和可追溯的处理参数。需要更细的方法、参数选择、常见错误和检查清单时，按任务读取下面的参考资料，不必把所有方法逐项执行。

## 参考资料

使用 reference_read 传入“vector-fields/references/文件.md”读取：

- 属性清洗：references/属性清洗.md
