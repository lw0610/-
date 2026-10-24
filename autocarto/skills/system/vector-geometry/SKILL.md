---
name: vector-geometry
description: 矢量几何处理、叠置和覆盖分析工具集合
kind: category
runtime: qgis
enabled: true
tags:
- vector
- geometry
tools:
- add_geometry_attributes
- bounding_boxes
- buffer
- centroids
- check_validity
- clip
- collect_geometries
- concave_hull
- convex_hull
- coverage_simplify
- coverage_union
- coverage_validate
- create_spatial_index
- delaunay_triangulation
- delete_duplicate_geometries
- densify_by_count
- densify_by_interval
- difference
- dissolve
- drop_z_values
- explode_lines
- extend_lines
- extract_by_extent
- extract_specific_vertices
- extract_vertices
- fix_geometries
- force_rhr
- geometry_by_expression
- interpolate_point_on_line
- intersection
- line_intersections
- line_substring
- lines_to_polygons
- merge_lines
- minimum_bounding_geometry
- minimum_enclosing_circle
- multipart_to_singleparts
- offset_line
- oriented_minimum_bounding_box
- point_on_surface
- points_along_geometry
- pole_of_inaccessibility
- polygons_to_lines
- remove_null_geometries
- reverse_line_direction
- rotate_features
- set_m_value
- set_z_value
- simplify
- single_sided_buffer
- smooth
- snap_geometries_to_layer
- snap_points_to_grid
- split_lines_by_maximum_length
- split_with_lines
- subdivide
- swap_xy_coordinates
- symmetric_difference
- tapered_buffer
- translate_geometry
- union
- variable_width_buffer
- voronoi_polygons
- wedge_buffers
- merge_vector_layers
display_name: 几何处理与叠加
domain: 矢量
system: false
hidden: false
auto_mount: ''
---

# 矢量几何工具集

本 package 收纳 矢量几何处理、叠置和覆盖分析工具集合，用于在运行时构建统一的扁平工具索引。

显示和叠加使用 QGIS 动态投影；只有分析、量算明确需要时才转换。先核实源 CRS、坐标单位和算法处理方式，不能因显示 CRS 不同而批量重投影；定义 CRS 只改元数据，重投影才改变坐标。转换时生成独立结果，核对范围、位置和精度。

这些工具用于相应数据的真实处理。先核实输入图层、字段、几何或波段、参数单位及输出位置；不要凭示例推断已有数据。执行后检查实际返回状态、真实产物、范围、类型及数量，空值和无效结果如实报告，保留原始数据和可追溯的处理参数。需要更细的方法、参数选择、常见错误和检查清单时，按任务读取下面的参考资料，不必把所有方法逐项执行。

## 参考资料

使用 reference_read 传入“vector-geometry/references/文件.md”读取：

- 缓冲与邻近分析：references/缓冲与邻近分析.md
- 几何修复：references/几何修复.md
- 叠加分析：references/叠加分析.md
