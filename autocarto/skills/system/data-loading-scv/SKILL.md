---
name: data-loading-scv
description: 行军线路图数据表处理流程，读取 CSV 文件并输出用户需要的数据。
kind: workflow
---

# 行军线路图数据表处理流程，读取 CSV 文件并输出用户需要的数据



## 执行步骤：
- 使用`pyqgis_execute_script`工具读取两个 CSV，确保编码正确（用 QGIS 的 delimitedtext provider 配合正确的 URI 格式）。
- 点表至少读取字段：name, symbol_type（其他字段可保留）。
- 符号表至少读取字段：symbol_type, svg_path, size, anchor_x, anchor_y, priority。
- 将两个 CSV 转成 point_rows / symbol_rows 数组。
