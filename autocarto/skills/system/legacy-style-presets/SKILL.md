---
name: legacy-style-presets
display_name: 旧样式预设兼容
kind: category
description: 旧预设入口仅供既有回归和迁移使用
runtime: qgis
domain: 制图
system: false
enabled: false
hidden: true
auto_mount: ''
tools:
- layer_set_fill_symbol
- layer_set_line_symbol
- layer_set_marker_symbol
---

旧入口在兼容期使用工具私有的冻结资产。新流程使用 `style_set_apply` 或 `layer_load_style`，默认资源由工具参数声明。
