---
name: route-cartography
description: 依据已校验的数据表绘制路线、点符号与图文标注。
kind: category
runtime: qgis
enabled: true
tools:
- route_draw
- route_points_build
display_name: 路线专题绘制
domain: 制图
system: false
hidden: false
auto_mount: ''
---

# 路线专题绘制

依据已校验的数据表绘制路线、点符号与图文标注。

新流程依次调用 route_draw 和 route_points_build。先确认数据集与行数断言；route_draw 的 route_style 选择路线样式资源名称、编号或路径，省略时使用工具声明的默认资源，数据集已有的非空 style_id 仍优先。route_points_build 的 symbols 可选择符号资源名称、编号或目录，省略时使用数据集中的 symbol_mapping.csv。符号图例说明来自资源的 description，缺省显示符号类型。调用后检查实际图层、资源摘要与失败项。图文标注交由版面编排的 layout_add_callouts。
