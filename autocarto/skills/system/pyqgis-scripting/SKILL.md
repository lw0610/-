---
name: pyqgis-scripting
description: PyQGIS 脚本执行、校验与任务管理工具集合
kind: category
runtime: qgis
enabled: true
tags:
- pyqgis
- scripting
tools:
- pyqgis_execute_script
- pyqgis_submit_script
- pyqgis_get_task_status
- pyqgis_get_context
- pyqgis_validate_script
- pyqgis_cancel_task
---

# PyQGIS 脚本工具集

本 package 收纳 PyQGIS 脚本执行、校验与任务管理工具集合，用于在运行时构建统一的扁平工具索引。

使用约束：

- 优先使用现成的交互式工具和数据加载/筛选工具；不要为了简单筛选、字段检查或图层存在性确认而直接写 PyQGIS 脚本
- 只有在现成工具无法表达当前操作，或需要一次性读取复杂上下文时，才使用 `pyqgis_validate_script` / `pyqgis_execute_script`
- 如果其他工具已经返回可直接消费的文件产物（例如 `bar_chart` 返回 `svg_path` / `picture_path`），优先直接把这些产物传给后续工具，禁止再写脚本把 HTML/已有文件二次转换
- 在脚本前必须先通过 `get_project_info`、`get_layers_info`、`get_active_layer_info` 或 `pyqgis_get_context` 确认真正缺失的信息，避免把脚本当成默认探测手段
