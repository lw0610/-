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
display_name: PyQGIS 脚本
domain: 脚本
system: false
hidden: false
auto_mount: ''
---

# PyQGIS 脚本使用说明

优先使用已分配的现成工具处理加载、筛选、字段和版面；只有操作不能由工具表达或确需复杂上下文时写脚本。先用工程/图层信息或 pyqgis_get_context 核实真实图层ID、字段、CRS和可用算法，不凭示例名猜测。现成 SVG 等产物直接传后续工具，不为转换已可用文件再写脚本。

先用 pyqgis_validate_script 检查，再按任务时长选 pyqgis_execute_script 或 pyqgis_submit_script；异步返回任务编号，使用 pyqgis_get_task_status 核验，取消时传真实编号。参数、输入、独立输出位置与数据修改范围应清楚；记录错误及部分完成状态，不能吞异常或无条件重复执行破坏性步骤。

显示和叠加使用 QGIS 动态投影；只有分析、量算明确需要时才转换。先核实源 CRS、坐标单位和算法处理方式，不能因显示 CRS 不同而批量重投影；定义 CRS 只改元数据，重投影才改变坐标。转换时生成独立结果，核对范围、位置和精度。

按方法读取下列 references，示例API需以当前QGIS提供者和参数定义核验。临时内存结果重要时另存文件，编辑事务失败回滚，输出通过真实图层/文件验证，不仅看打印的成功消息。

若脚本是在纠正本会话生成的错误 GeoJSON，修正结果先保留在内存图层，再通过 data_export_vector 的 replace_source 指定原文件进行验证和替换；不要先导出 `_clean` 副本，也不要自行删除原文件。外部原始输入和独立分析结果仍按各自用途保留，不凭名称判断替换对象。


## 参考资料

使用 reference_read 传入“pyqgis-scripting/references/文件.md”读取：

- pyqgis-attribute-ops：references/pyqgis-attribute-ops.md
- pyqgis-batch-processing：references/pyqgis-batch-processing.md
- pyqgis-crs-transform：references/pyqgis-crs-transform.md
- pyqgis-data-loading：references/pyqgis-data-loading.md
- pyqgis-geometry-ops：references/pyqgis-geometry-ops.md
- pyqgis-interpolation：references/pyqgis-interpolation.md
- pyqgis-layout：references/pyqgis-layout.md
- pyqgis-raster-analysis：references/pyqgis-raster-analysis.md
- pyqgis-spatial-query：references/pyqgis-spatial-query.md
- pyqgis-styling：references/pyqgis-styling.md
- pyqgis-terrain-ops：references/pyqgis-terrain-ops.md
- pyqgis-vector-analysis：references/pyqgis-vector-analysis.md
- pyqgis-workflow：references/pyqgis-workflow.md
