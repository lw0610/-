---
name: network-analysis
description: 路径分析与服务区计算工具集合
kind: category
runtime: qgis
enabled: true
tags:
- network
- analysis
tools:
- service_area_from_layer
- service_area_from_point
- shortest_path_layer_to_point
- shortest_path_point_to_layer
- shortest_path_point_to_point
display_name: 网络分析
domain: 栅格与分析
system: false
hidden: false
auto_mount: ''
---

# 网络分析使用说明

确认真实路网、起终点或设施图层、拓扑连通、方向限制和成本字段。check_validity 只能检查几何，不能证明道路已连通；路口未打断、单行方向或匹配容差不当都可能使路径失败。速度字段单位、缺失值和默认速度需明确，不能把示意速度报告为实测值。

点到点、点到图层、图层到点分别选 shortest_path_point_to_point、shortest_path_point_to_layer、shortest_path_layer_to_point；服务范围使用 service_area_from_point/from_layer。距离最短与时间最短不同，阈值按工具实际单位传入。先核对起终点到路网的连接，结果异常时检查连通性和成本，避免无依据改变输入位置。

显示和叠加使用 QGIS 动态投影；只有分析、量算明确需要时才转换。先核实源 CRS、坐标单位和算法处理方式，不能因显示 CRS 不同而批量重投影；定义 CRS 只改元数据，重投影才改变坐标。转换时生成独立结果，核对范围、位置和精度。

检查路径起止、连续性、长度、时间或可达范围，报告真实成本模型和未连接对象。进一步方法：references/可达性与服务区.md，可通过 reference_read 读取。

## 参考资料

使用 reference_read 传入“network-analysis/references/文件.md”读取：

- 可达性与服务区：references/可达性与服务区.md
