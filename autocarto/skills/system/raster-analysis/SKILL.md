---
name: raster-analysis
description: 栅格分析、插值与统计处理工具集合
kind: category
runtime: qgis
enabled: true
tags:
- raster
- analysis
tools:
- aspect
- create_constant_raster
- create_random_raster_uniform
- heatmap
- hillshade
- idw_interpolation
- raster_reclassify
- raster_sample
- raster_statistics
- slope
- tin_interpolation
display_name: 栅格分析
domain: 栅格与分析
system: false
hidden: false
auto_mount: ''
---

# 栅格分析工具集

本 package 收纳 栅格分析、插值与统计处理工具集合，用于在运行时构建统一的扁平工具索引。

显示和叠加使用 QGIS 动态投影；只有分析、量算明确需要时才转换。先核实源 CRS、坐标单位和算法处理方式，不能因显示 CRS 不同而批量重投影；定义 CRS 只改元数据，重投影才改变坐标。转换时生成独立结果，核对范围、位置和精度。

这些工具用于相应数据的真实处理。先核实输入图层、字段、几何或波段、参数单位及输出位置；不要凭示例推断已有数据。执行后检查实际返回状态、真实产物、范围、类型及数量，空值和无效结果如实报告，保留原始数据和可追溯的处理参数。需要更细的方法、参数选择、常见错误和检查清单时，按任务读取下面的参考资料，不必把所有方法逐项执行。

## 参考资料

使用 reference_read 传入“raster-analysis/references/文件.md”读取：

- 地形衍生：references/地形衍生.md
- 空间插值：references/空间插值.md
- 土地利用变化检测：references/土地利用变化检测.md
- 栅格代数：references/栅格代数.md
- 重分类与分区统计：references/重分类与分区统计.md
- 选址适宜性分析：references/选址适宜性分析.md
