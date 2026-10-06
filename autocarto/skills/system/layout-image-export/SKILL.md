---
name: layout-image-export
description: 布局图片导出工具，仅在用户明确要求图片格式时加载
kind: category
runtime: qgis
enabled: true
tags:
- layout
- image-export
tools:
- export_print_layout
---

# 布局图片导出

本 package 仅收纳打印布局图片导出工具。

- 只有在用户明确要求 PNG/JPEG/图片预览时才应加载和使用。
- 默认打印地图工作流应优先只导出 PDF。
