---
name: point-cloud
description: 点云裁剪、转换、统计与栅格化工具集合
kind: category
runtime: qgis
enabled: true
tags:
- pointcloud
- processing
tools:
- pdal_clip
- pdal_convert
- pdal_info
- pdal_merge
- pdal_to_raster
display_name: 点云
domain: 栅格与分析
system: false
hidden: false
auto_mount: ''
---

# 点云使用说明

先用 pdal_info 核实点数、范围、CRS、点密度、分类与高程单位，确认 LAS/LAZ/PLY 等格式当前驱动支持。数据缺少 CRS 时从可靠元信息确认，不能随意定义。按需用 pdal_clip 裁剪、pdal_merge 合并、pdal_convert 转格式；合并前确认单位、分类和坐标含义兼容。

pdal_to_raster 生成 DEM 时需要合适的地面点筛选，DSM 则需明确表面高程选择；不能仅按文件名断言已有地面分类。像元大小根据有效点密度与成果尺度选择，升采样不能创造地表细节。空洞可能由点密度、分类筛选或掩膜导致，应定位原因而非默认填补。

显示和叠加使用 QGIS 动态投影；只有分析、量算明确需要时才转换。先核实源 CRS、坐标单位和算法处理方式，不能因显示 CRS 不同而批量重投影；定义 CRS 只改元数据，重投影才改变坐标。转换时生成独立结果，核对范围、位置和精度。

核对处理前后点数、范围、输出分辨率、值域和NoData，噪声高点和分类问题须报告。大数据先限定明确范围和产物，保留处理参数与原始文件。
