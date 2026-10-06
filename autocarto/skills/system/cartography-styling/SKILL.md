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
- cartography_set_style
- cartography_graduated_style
- cartography_add_labels
- layer_get_style_info
- layer_set_marker_symbol
- layer_set_line_symbol
- layer_set_fill_symbol
- cartography_build_and_draw_from_tables
- import_layout_template
---
# 制图样式工具集

本 package 收纳地图符号化与布局模板导入工具，用于在运行时构建统一的扁平工具索引。

当用户明确提供 QGIS `.qpt` 模板文件时，可调用 `import_layout_template` 将其导入指定布局。该工具只负责导入或覆盖布局，不会自动替代现有排版 workflow。
