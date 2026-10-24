---
name: spatial-analysis
description: 聚类、距离与地形指标分析工具集合
kind: category
runtime: qgis
enabled: true
tags:
- spatial
- analysis
tools:
- dbscan_clustering
- distance_matrix
- distance_to_nearest_hub
- kmeans_clustering
- nearest_neighbor
- terrain_ruggedness
- terrain_tpi
display_name: 空间统计与聚类
domain: 栅格与分析
system: false
hidden: false
auto_mount: ''
---

# 空间统计与聚类使用说明

先确认真实点图层、数量、空间覆盖、重复位置和字段含义，判断采样过程是否影响结论。距离型分析核实单位和算法量算方式；不能凭图层看起来分散就解释为统计显著性。

DBSCAN 适合密度相连且形状不规则的簇，dbscan_clustering 的邻域半径与最小点数须结合密度和尺度选择；半径过大可能合成一簇，过小或最小点数过大可能全为噪声。kmeans_clustering 用于指定聚类数的划分，比较不同 k 的稳定性，不能把聚类标签当因果机制。

显示和叠加使用 QGIS 动态投影；只有分析、量算明确需要时才转换。先核实源 CRS、坐标单位和算法处理方式，不能因显示 CRS 不同而批量重投影；定义 CRS 只改元数据，重投影才改变坐标。转换时生成独立结果，核对范围、位置和精度。

检查聚类标签、每簇数量、噪声、中心与空间分布，并记录参数和输入过滤。参数敏感性和稀疏区不确定性应说明。统计检验的假设、分母和显著性须与实际工具输出一致，不把视觉热点当显著聚集。
