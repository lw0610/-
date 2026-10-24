---
name: visibility-analysis
description: 通视和视域分析工具集合
kind: category
runtime: qgis
enabled: true
tags:
- visibility
- analysis
tools:
- line_of_sight
- viewshed
display_name: 可视域分析
domain: 栅格与分析
system: false
hidden: false
auto_mount: ''
---

# 可视域分析使用说明

核实 DEM 的范围、分辨率、高程与水平单位，明确观察点坐标、观察高度、目标高度和最大距离。高度是离地高还是绝对高程须以当前工具参数为准；DEM 边界应覆盖分析范围，地形遮挡结论不能忽略数据空洞。

viewshed 用于给定观察位置的可视域，line_of_sight 用于两点通视。半径和输出网格按数据分辨率与需求设置，超大半径会增加计算量并引入曲率和折射等影响，相关选项应按算法能力记录。多点累计时在共同网格上叠加，区分未知/NoData与不可见，不把二者都计为零。

显示和叠加使用 QGIS 动态投影；只有分析、量算明确需要时才转换。先核实源 CRS、坐标单位和算法处理方式，不能因显示 CRS 不同而批量重投影；定义 CRS 只改元数据，重投影才改变坐标。转换时生成独立结果，核对范围、位置和精度。

检查值域约定、有效覆盖、观察点位置、边缘截断及可视面积分母。全部可视或不可视时先查输入高度、范围和数据值，不随意调参追求图面效果。结果报告真实限制、参数与统计。
