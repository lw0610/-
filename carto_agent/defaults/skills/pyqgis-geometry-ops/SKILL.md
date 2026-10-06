---
name: pyqgis-geometry-ops
description: PyQGIS 几何操作脚本模板，支持几何创建、修复、变换和空间索引操作
kind: workflow
---

# PyQGIS 几何操作

## 概述

本 skill 提供 PyQGIS 脚本模板，用于几何对象的创建、修复、变换和空间索引操作。直接使用 QgsGeometry API 进行底层几何操作。

## 核心 API 参考

| 类/方法 | 用途 |
|---------|------|
| `QgsGeometry.fromPointXY(point)` | 从点创建几何 |
| `QgsGeometry.fromPolylineXY(points)` | 从点序列创建线 |
| `QgsGeometry.fromPolygonXY(rings)` | 从环创建面 |
| `geom.buffer(distance, segments)` | 缓冲区 |
| `geom.simplify(tolerance)` | 简化 |
| `geom.centroid()` | 质心 |
| `geom.convexHull()` | 凸包 |
| `geom.makeValid()` | 修复无效几何 |
| `geom.intersection(other)` | 几何交集 |
| `geom.combine(other)` | 几何合并 |
| `QgsSpatialIndex()` | 空间索引 |

## 代码模板

### 1. 几何创建

```python
from qgis.core import (
    QgsGeometry, QgsPointXY, QgsFeature, QgsVectorLayer,
    QgsProject, QgsField, QgsFields
)
from qgis.PyQt.QtCore import QVariant

# 创建点
point_geom = QgsGeometry.fromPointXY(QgsPointXY(116.397, 39.908))

# 创建线
line_points = [QgsPointXY(0, 0), QgsPointXY(1, 1), QgsPointXY(2, 0)]
line_geom = QgsGeometry.fromPolylineXY(line_points)

# 创建面
ring = [QgsPointXY(0, 0), QgsPointXY(0, 1), QgsPointXY(1, 1), QgsPointXY(1, 0), QgsPointXY(0, 0)]
polygon_geom = QgsGeometry.fromPolygonXY([ring])

# 创建带孔的面
outer_ring = [QgsPointXY(0, 0), QgsPointXY(0, 10), QgsPointXY(10, 10), QgsPointXY(10, 0), QgsPointXY(0, 0)]
inner_ring = [QgsPointXY(3, 3), QgsPointXY(3, 7), QgsPointXY(7, 7), QgsPointXY(7, 3), QgsPointXY(3, 3)]
polygon_with_hole = QgsGeometry.fromPolygonXY([outer_ring, inner_ring])

# 将几何写入新图层
mem_layer = QgsVectorLayer("Point?crs=EPSG:4326", "new_points", "memory")
provider = mem_layer.dataProvider()
provider.addAttributes([QgsField("name", QVariant.String)])
mem_layer.updateFields()

feat = QgsFeature()
feat.setGeometry(point_geom)
feat.setAttributes(["test_point"])
provider.addFeature(feat)

QgsProject.instance().addMapLayer(mem_layer)
```

### 2. 几何修复

```python
import processing
from qgis.core import QgsProject

layer = QgsProject.instance().mapLayersByName("input_layer")[0]

# 方法1：使用 processing
result = processing.run("native:fixgeometries", {
    'INPUT': layer,
    'METHOD': 1,  # 0=基于结构, 1=基于 linework
    'OUTPUT': 'memory:fixed_geometries'
})
QgsProject.instance().addMapLayer(result['OUTPUT'])

# 方法2：逐要素修复（更精细控制）
fixed_count = 0
layer.startEditing()
for feat in layer.getFeatures():
    geom = feat.geometry()
    if not geom.isNull() and not geom.isGeosValid():
        fixed_geom = geom.makeValid()
        if fixed_geom.isGeosValid():
            layer.changeGeometry(feat.id(), fixed_geom)
            fixed_count += 1
layer.commitChanges()
print(f"修复了 {fixed_count} 个无效几何")
```

### 3. 质心与凸包提取

```python
from qgis.core import QgsProject, QgsVectorLayer, QgsFeature, QgsField
from qgis.PyQt.QtCore import QVariant

layer = QgsProject.instance().mapLayersByName("polygons")[0]

# 提取质心
centroid_layer = QgsVectorLayer("Point?crs=" + layer.crs().authid(), "centroids", "memory")
provider = centroid_layer.dataProvider()
provider.addAttributes([QgsField("source_id", QVariant.Int), QgsField("source_name", QVariant.String)])
centroid_layer.updateFields()

for feat in layer.getFeatures():
    centroid_feat = QgsFeature()
    centroid_feat.setGeometry(feat.geometry().centroid())
    centroid_feat.setAttributes([feat.id(), feat["name"] if "name" in [f.name() for f in layer.fields()] else ""])
    provider.addFeature(centroid_feat)

QgsProject.instance().addMapLayer(centroid_layer)

# 计算所有要素的凸包
all_geoms = QgsGeometry()
for feat in layer.getFeatures():
    if all_geoms.isNull():
        all_geoms = feat.geometry()
    else:
        all_geoms = all_geoms.combine(feat.geometry())

convex_hull = all_geoms.convexHull()

hull_layer = QgsVectorLayer("Polygon?crs=" + layer.crs().authid(), "convex_hull", "memory")
hull_provider = hull_layer.dataProvider()
hull_feat = QgsFeature()
hull_feat.setGeometry(convex_hull)
hull_provider.addFeature(hull_feat)
QgsProject.instance().addMapLayer(hull_layer)
```

### 4. 空间索引加速查询

```python
from qgis.core import QgsProject, QgsSpatialIndex, QgsGeometry, QgsPointXY

layer = QgsProject.instance().mapLayersByName("buildings")[0]

# 构建空间索引
index = QgsSpatialIndex(layer.getFeatures())

# 查找某点附近的要素
search_point = QgsPointXY(116.397, 39.908)
search_geom = QgsGeometry.fromPointXY(search_point)

# 最近邻查询（返回最近的 5 个要素 ID）
nearest_ids = index.nearestNeighbor(search_point, 5)
print(f"最近的 5 个要素 ID: {nearest_ids}")

# 矩形范围查询
search_rect = search_geom.buffer(0.01, 16).boundingBox()
candidate_ids = index.intersects(search_rect)

# 精确空间过滤
for fid in candidate_ids:
    feat = layer.getFeature(fid)
    if feat.geometry().distance(search_geom) < 0.005:
        print(f"要素 {fid}: 距离 {feat.geometry().distance(search_geom):.6f}")
```

### 5. 几何合并与融合

```python
import processing
from qgis.core import QgsProject, QgsGeometry

layer = QgsProject.instance().mapLayersByName("parcels")[0]

# 方法1：按属性融合（dissolve）
result = processing.run("native:dissolve", {
    'INPUT': layer,
    'FIELD': ['district'],  # 按 district 字段融合
    'OUTPUT': 'memory:dissolved'
})
QgsProject.instance().addMapLayer(result['OUTPUT'])

# 方法2：手动合并所有几何
combined = QgsGeometry()
for feat in layer.getFeatures():
    if combined.isNull():
        combined = QgsGeometry(feat.geometry())
    else:
        combined = combined.combine(feat.geometry())

print(f"合并后几何类型: {combined.type()}, 面积: {combined.area():.2f}")
```

## 脚本编写规范

1. 几何操作前检查 `geom.isNull()` 和 `geom.isGeosValid()`。
2. 创建内存图层时必须指定 CRS：`"Point?crs=EPSG:4326"`。
3. 添加要素到内存图层使用 `dataProvider().addFeature()`，不需要编辑会话。
4. 大量要素的空间查询必须使用 `QgsSpatialIndex` 加速。
5. 几何合并操作在大数据量下内存消耗大，建议分批处理。

## 常见陷阱

- `QgsGeometry.fromPolygonXY()` 的环必须闭合（首尾点相同）
- `centroid()` 返回的质心可能在多边形外部（如 C 形多边形），用 `pointOnSurface()` 更安全
- `combine()` 逐个合并效率低，大量几何建议用 `QgsGeometry.unaryUnion(geom_list)`
- 内存图层的 `dataProvider().addFeature()` 不需要 `startEditing()`，但 `changeGeometry()` 需要
- `QgsSpatialIndex.intersects()` 返回的是边界框相交的候选集，需要进一步精确判断
