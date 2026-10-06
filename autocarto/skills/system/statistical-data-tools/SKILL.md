---
name: statistical-data-tools
description: 行政区年度统计数据核验与独立派生工具，仅供数据工程角色使用
kind: category
runtime: qgis
enabled: true
tools:
- data_prepare_statistical
---

# 统计数据准备工具

`data_prepare_statistical` 检查行政区 GeoJSON 与年度 CSV 的关联、指标口径和数值状态，并生成独立派生数据及 manifest。不会自动加载工程，不覆盖源文件；完整使用规则见 `statistical-data-preparation`。
