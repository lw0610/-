---
name: pyqgis-vector-analysis
description: PyQGIS 矢量空间分析脚本模板，支持叠加分析、缓冲区、距离分析、网络分析和聚类分析
kind: workflow
---

# PyQGIS 矢量空间分析

## 概述

本 skill 提供 PyQGIS 脚本模板，用于矢量空间分析操作。通过 `processing.run()` 调用 QGIS/GDAL/GRASS 算法，实现叠加分析、缓冲区、距离矩阵、网络分析和聚类分析。

## 核心 API 参考

| 类/方法 | 用途 |
|---------|------|
| `processing.run(algorithm, params)` | 执行 Processing 算法 |
| `processing.algorithmHelp(algorithm)` | 查看算法帮助 |
| `QgsProcessingFeedback()` | 处理进度反馈 |
| `QgsVectorLayer` | 矢量图层操作 |
| `QgsFeatureRequest` | 要素查询请求 |

## 代码模板

### 1. 叠加分析（交集/并集/差集）

```python
import processing
from qgis.core import QgsProject

layer_a = QgsProject.instance().mapLayersByName("layer_a")[0]
layer_b = QgsProject.instance().mapLayersByName("layer_b")[0]

# 交集
result_intersect = processing.run("native:intersection", {
    'INPUT': layer_a,
    'OVERLAY': layer_b,
    'INPUT_FIELDS': [],
    'OVERLAY_FIELDS': [],
    'OUTPUT': 'memory:intersection_result'
})
QgsProject.instance().addMapLayer(result_intersect['OUTPUT'])

# 并集
result_union = processing.run("native:union", {
    'INPUT': layer_a,
    'OVERLAY': layer_b,
    'OUTPUT': 'memory:union_result'
})
QgsProject.instance().addMapLayer(result_union['OUTPUT'])

# 差集
result_diff = processing.run("native:difference", {
    'INPUT': layer_a,
    'OVERLAY': layer_b,
    'OUTPUT': 'memory:difference_result'
})
QgsProject.instance().addMapLayer(result_diff['OUTPUT'])

# 裁剪
result_clip = processing.run("native:clip", {
    'INPUT': layer_a,
    'OVERLAY': layer_b,
    'OUTPUT': 'memory:clip_result'
})
QgsProject.instance().addMapLayer(result_clip['OUTPUT'])

print(f"交集要素数: {result_intersect['OUTPUT'].featureCount()}")
print(f"并集要素数: {result_union['OUTPUT'].featureCount()}")
print(f"差集要素数: {result_diff['OUTPUT'].featureCount()}")
```

### 2. 缓冲区分析

```python
import processing
from qgis.core import QgsProject

layer = QgsProject.instance().mapLayersByName("input_layer")[0]

# 标准缓冲区
result = processing.run("native:buffer", {
    'INPUT': layer,
    'DISTANCE': 1000,        # 距离，单位取决于 CRS（投影坐标系=米）
    'SEGMENTS': 16,          # 圆弧段数
    'END_CAP_STYLE': 0,      # 0=圆形, 1=平头, 2=方形
    'JOIN_STYLE': 0,         # 0=圆形, 1=斜接, 2=斜切
    'MITER_LIMIT': 2,
    'DISSOLVE': True,        # 是否融合重叠缓冲区
    'OUTPUT': 'memory:buffer_1km'
})
QgsProject.instance().addMapLayer(result['OUTPUT'])

# 多级缓冲区
distances = [500, 1000, 2000, 5000]
for dist in distances:
    result = processing.run("native:buffer", {
        'INPUT': layer,
        'DISTANCE': dist,
        'DISSOLVE': True,
        'OUTPUT': f'memory:buffer_{dist}m'
    })
    QgsProject.instance().addMapLayer(result['OUTPUT'])
    print(f"缓冲区 {dist}m: {result['OUTPUT'].featureCount()} 个要素")
```

### 3. 距离矩阵

```python
import processing
from qgis.core import QgsProject

points = QgsProject.instance().mapLayersByName("points")[0]
targets = QgsProject.instance().mapLayersByName("targets")[0]

# 距离矩阵
result = processing.run("native:distancematrix", {
    'INPUT': points,
    'INPUT_FIELD': 'id',
    'TARGET': targets,
    'TARGET_FIELD': 'id',
    'MATRIX_TYPE': 0,  # 0=线性(N*k), 1=标准(N*T), 2=汇总(N*1)
    'NEAREST_POINTS': 3,  # 最近的 k 个点
    'OUTPUT': 'memory:distance_matrix'
})
QgsProject.instance().addMapLayer(result['OUTPUT'])

# 最近设施分析
result_hub = processing.run("native:distancetonearesthubpoints", {
    'INPUT': points,
    'HUBS': targets,
    'HUBS_FIELD': 'name',
    'UNIT': 0,  # 0=米
    'OUTPUT': 'memory:nearest_hub'
})
QgsProject.instance().addMapLayer(result_hub['OUTPUT'])
```

### 4. 网络分析

```python
import processing
from qgis.core import QgsProject, QgsPointXY

road_network = QgsProject.instance().mapLayersByName("roads")[0]

# 最短路径（点到点）
result = processing.run("native:shortestpathpointtopoint", {
    'INPUT': road_network,
    'STRATEGY': 0,           # 0=最短距离, 1=最短时间
    'DIRECTION_FIELD': '',
    'VALUE_FORWARD': '',
    'VALUE_BACKWARD': '',
    'VALUE_BOTH': '',
    'DEFAULT_DIRECTION': 2,  # 2=双向
    'SPEED_FIELD': '',
    'DEFAULT_SPEED': 50,     # 默认速度 km/h
    'TOLERANCE': 0,
    'START_POINT': '116.397,39.908 [EPSG:4326]',
    'END_POINT': '116.467,39.918 [EPSG:4326]',
    'OUTPUT': 'memory:shortest_path'
})
QgsProject.instance().addMapLayer(result['OUTPUT'])

# 服务区分析
result_sa = processing.run("native:serviceareafrompoint", {
    'INPUT': road_network,
    'STRATEGY': 0,
    'DIRECTION_FIELD': '',
    'VALUE_FORWARD': '',
    'VALUE_BACKWARD': '',
    'VALUE_BOTH': '',
    'DEFAULT_DIRECTION': 2,
    'SPEED_FIELD': '',
    'DEFAULT_SPEED': 50,
    'TOLERANCE': 0,
    'START_POINT': '116.397,39.908 [EPSG:4326]',
    'TRAVEL_COST': 5000,  # 距离（米）或时间（秒）
    'OUTPUT': 'memory:service_area'
})
QgsProject.instance().addMapLayer(result_sa['OUTPUT'])
```

### 5. 聚类分析

```python
import processing
from qgis.core import QgsProject

points = QgsProject.instance().mapLayersByName("points")[0]

# DBSCAN 密度聚类
result_dbscan = processing.run("native:dbscanclustering", {
    'INPUT': points,
    'MIN_SIZE': 5,       # 最小簇大小
    'EPS': 1000,         # 邻域半径（单位取决于 CRS）
    'DBSCAN*': False,
    'FIELD_NAME': 'CLUSTER_ID',
    'SIZE_FIELD_NAME': 'CLUSTER_SIZE',
    'OUTPUT': 'memory:dbscan_result'
})
QgsProject.instance().addMapLayer(result_dbscan['OUTPUT'])

# K-Means 聚类
result_kmeans = processing.run("native:kmeansclustering", {
    'INPUT': points,
    'CLUSTERS': 5,       # 聚类数
    'FIELD_NAME': 'CLUSTER_ID',
    'SIZE_FIELD_NAME': 'CLUSTER_SIZE',
    'OUTPUT': 'memory:kmeans_result'
})
QgsProject.instance().addMapLayer(result_kmeans['OUTPUT'])

# 统计聚类结果
cluster_layer = result_dbscan['OUTPUT']
clusters = set()
noise = 0
for feat in cluster_layer.getFeatures():
    cid = feat['CLUSTER_ID']
    if cid == 0:
        noise += 1
    else:
        clusters.add(cid)
print(f"DBSCAN: {len(clusters)} 个簇, {noise} 个噪声点")
```

## 脚本编写规范

1. 叠加分析前确认两个图层的 CRS 一致，不一致时先重投影。
2. 缓冲区距离单位取决于图层 CRS：投影坐标系为米，地理坐标系为度。
3. 使用 `'memory:'` 前缀创建内存图层，避免生成临时文件。
4. 大数据量分析时使用 `QgsProcessingFeedback()` 监控进度。
5. 网络分析的起止点格式为 `'x,y [CRS]'`，注意坐标顺序。

## 常见陷阱

- 叠加分析中无效几何会导致失败，建议先运行 `native:fixgeometries`
- 缓冲区在地理坐标系下使用度为单位，1度≈111km，容易出错
- 距离矩阵在大数据量下（>10000点）计算量巨大，建议限制 `NEAREST_POINTS`
- 网络分析要求路网图层为线类型且拓扑连通
- DBSCAN 的 `EPS` 参数对结果影响极大，需根据数据分布调整
- `processing.run()` 返回的 `OUTPUT` 可能是文件路径字符串或图层对象，取决于输出参数设置
