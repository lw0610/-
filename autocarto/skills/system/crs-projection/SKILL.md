---
name: crs-projection
description: 坐标参考系查询、定义与重投影工具集合
kind: category
runtime: qgis
enabled: true
tags:
- crs
- projection
tools:
- crs_define_projection
- crs_find_projection
- crs_reproject_vector
- define_shapefile_projection
- find_projection
- reproject_layer
display_name: 坐标系
domain: 数据
system: false
hidden: false
auto_mount: ''
---

# 坐标系使用说明

显示和叠加使用 QGIS 动态投影；只有分析、量算明确需要时才转换。先核实源 CRS、坐标单位和算法处理方式，不能因显示 CRS 不同而批量重投影；定义 CRS 只改元数据，重投影才改变坐标。转换时生成独立结果，核对范围、位置和精度。

用 get_layers_info 和真实图层详情记录 CRS、范围和坐标单位；CRS 缺失时先查数据元信息，不能直接把 EPSG:4326 指给未知坐标。crs_find_projection 只能提供候选，须结合数据来源与位置验证。crs_define_projection/define_shapefile_projection 用于有依据的定义；reproject_layer/crs_reproject_vector 用于真实转换。

按研究范围、误差需求及算法单位选择目标 CRS，不把一个投影带用到所有地区。距离/面积计算要说明平面或椭球量算方式。栅格重采样按连续值或类别选择，转换后检查位置、范围、数量和精度，不把所有图层 CRS 相同作为唯一验收。
