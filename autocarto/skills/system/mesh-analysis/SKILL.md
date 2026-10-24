---
name: mesh-analysis
description: 处理网格数据、提取等值线与剖面并导出时间序列。
kind: category
runtime: qgis
enabled: true
tools:
- export_mesh_edges
- export_mesh_on_grid
- export_cross_section_dataset_values
- export_time_series_values
- tin_mesh_creation
- generate_elevation_profile_image
- export_mesh_faces
- export_mesh_vertices
- export_mesh_contours
- mesh_surface_to_polygon
- rasterize_mesh_dataset
display_name: 网格与剖面
domain: 栅格与分析
system: false
hidden: false
auto_mount: ''
---

# 网格与剖面使用说明

先确认网格文件是否为当前驱动支持格式，核实已加载图层、顶点/边/面结构、数据集组、分量、垂向层和时间步。不能把数据集索引、时间索引与实际日期混用；输出值的物理单位和缺测规则须沿用来源。

需要几何组件时用 export_mesh_vertices/export_mesh_edges/export_mesh_faces；需要栅格时用 rasterize_mesh_dataset 或 export_mesh_on_grid，分辨率应匹配网格尺度，范围应在有效覆盖内。mesh_surface_to_polygon 和 export_mesh_contours 用于对应曲面表达，tin_mesh_creation 按真实输入构建网格。剖面、点时序与图像分别使用 export_cross_section_dataset_values、export_time_series_values、generate_elevation_profile_image。

检查输出类型、范围、记录数量、时间和目标数据集是否正确。大量 NoData 先查范围、索引和垂向层；不能只因文件生成就宣布提取完成。保留方法、插值/栅格化参数及真实产物路径。
