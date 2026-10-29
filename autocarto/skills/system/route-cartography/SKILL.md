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

依次调用 route_draw 和 route_points_build，使用本次确认后的数据集。route_draw 默认采用数据集已有 style_id 与原版内置渲染规则；route_points_build 默认采用数据集生成的 symbol_mapping.csv。仅用户明确指定使用外部路线样式表或符号表时，才填写可选的 route_style 或 symbols 预设名称，不填写资源路径，也不因要求红蓝配色自动选表。显式选择的表不可用时说明具体原因。符号图例说明来自 symbol_mapping.csv 的 description，缺省显示符号类型。调用后检查实际图层与失败项。图文标注交由版面编排的 layout_add_callouts。
