---
name: pyqgis-spatial-query
description: PyQGIS 空间查询与要素筛选脚本模板，支持属性查询、空间过滤、选择集操作和要素提取
kind: workflow
---

# PyQGIS 空间查询与要素筛选

## 概述

本 skill 提供 PyQGIS 脚本模板，用于属性查询、空间过滤、选择集操作和要素提取。涵盖 QgsExpression、QgsFeatureRequest 和 QgsSpatialIndex 的使用。

## 核心 API 参考

| 类/方法 | 用途 |
|---------|------|
| `QgsExpression(expr_string)` | 创建查询表达式 |
| `QgsFeatureRequest()` | 创建要素请求 |
| `request.setFilterExpression(expr)` | 设置表达式过滤 |
| `request.setFilterRect(rect)` | 设置空间范围过滤 |
| `layer.getFeatures(request)` | 按条件获取要素 |
| `layer.selectByExpression(expr)` | 按表达式选择 |
| `layer.selectByIds(ids)` | 按 ID 选择 |
| `layer.removeSelection()` | 清除选择 |
| `QgsSpatialIndex(features)` | 构建空间索引 |

## 代码模板

### 1. 属性查询与筛选

```python
from qgis.core import QgsProject, QgsFeatureRequest, QgsExpression

layer = QgsProject.instance().mapLayersByName("cities")[0]

# 简单属性过滤
request = QgsFeatureRequest().setFilterExpression('"population" > 500000')
large_cities = list(layer.getFeatures(request))
print(f"人口>50万的城市: {len(large_cities)} 个")

# 组合条件
expr = '"population" > 100000 AND "province" = \'广东\''
request = QgsFeatureRequest().setFilterExpression(expr)
for feat in layer.getFeatures(request):
    print(f"  {feat['name']}: 人口 {feat['population']}")

# 模糊匹配
expr_like = '"name" LIKE \'%市%\''
request = QgsFeatureRequest().setFilterExpression(expr_like)
count = sum(1 for _ in layer.getFeatures(request))
print(f"名称含'市'的要素: {count} 个")

# 限制返回字段（减少内存）
request = QgsFeatureRequest()
request.setFilterExpression('"population" > 1000000')
request.setSubsetOfAttributes(['name', 'population'], layer.fields())
request.setLimit(10)
for feat in layer.getFeatures(request):
    print(f"  {feat['name']}")
```

### 2. 空间范围过滤

```python
from qgis.core import QgsProject, QgsFeatureRequest, QgsRectangle, QgsGeometry, QgsPointXY

layer = QgsProject.instance().mapLayersByName("buildings")[0]

# 矩形范围过滤
bbox = QgsRectangle(116.3, 39.8, 116.5, 40.0)
request = QgsFeatureRequest().setFilterRect(bbox)
features_in_bbox = list(layer.getFeatures(request))
print(f"范围内要素: {len(features_in_bbox)} 个")

# 组合空间+属性过滤
request = QgsFeatureRequest()
request.setFilterRect(bbox)
request.setFilterExpression('"type" = \'residential\'')
residential = list(layer.getFeatures(request))
print(f"范围内住宅: {len(residential)} 个")
```

### 3. 空间相交查询

```python
from qgis.core import (
    QgsProject, QgsSpatialIndex, QgsGeometry, QgsPointXY,
    QgsVectorLayer, QgsFeature, QgsField
)
from qgis.PyQt.QtCore import QVariant

target_layer = QgsProject.instance().mapLayersByName("parcels")[0]
query_layer = QgsProject.instance().mapLayersByName("flood_zone")[0]

# 获取查询几何（洪水区域）
flood_geom = QgsGeometry()
for feat in query_layer.getFeatures():
    if flood_geom.isNull():
        flood_geom = QgsGeometry(feat.geometry())
    else:
        flood_geom = flood_geom.combine(feat.geometry())

# 使用空间索引加速
index = QgsSpatialIndex(target_layer.getFeatures())
candidate_ids = index.intersects(flood_geom.boundingBox())

# 精确相交判断
intersecting_features = []
for fid in candidate_ids:
    feat = target_layer.getFeature(fid)
    if feat.geometry().intersects(flood_geom):
        intersecting_features.append(feat)

print(f"与洪水区域相交的地块: {len(intersecting_features)} 个")

# 提取到新图层
result_layer = QgsVectorLayer(
    f"Polygon?crs={target_layer.crs().authid()}", "flood_affected_parcels", "memory"
)
provider = result_layer.dataProvider()
provider.addAttributes(target_layer.fields().toList())
result_layer.updateFields()
provider.addFeatures(intersecting_features)
QgsProject.instance().addMapLayer(result_layer)
```

### 4. 选择集操作

```python
from qgis.core import QgsProject, QgsVectorLayer

layer = QgsProject.instance().mapLayersByName("target_layer")[0]

# 按表达式选择
layer.selectByExpression('"area" > 1000', QgsVectorLayer.SetSelection)
print(f"选中: {layer.selectedFeatureCount()} 个要素")

# 追加选择
layer.selectByExpression('"type" = \'park\'', QgsVectorLayer.AddToSelection)
print(f"追加后选中: {layer.selectedFeatureCount()} 个要素")

# 从选择中移除
layer.selectByExpression('"status" = \'closed\'', QgsVectorLayer.RemoveFromSelection)

# 反选
layer.invertSelection()

# 获取选中要素
for feat in layer.selectedFeatures():
    print(f"  ID={feat.id()}, name={feat['name']}")

# 清除选择
layer.removeSelection()
```

### 5. 要素提取到新图层

```python
import processing
from qgis.core import QgsProject

layer = QgsProject.instance().mapLayersByName("input_layer")[0]

# 按属性提取
result = processing.run("native:extractbyattribute", {
    'INPUT': layer,
    'FIELD': 'type',
    'OPERATOR': 0,  # 0=等于, 1=不等于, 2=大于, 3=大于等于, 4=小于, 5=小于等于
    'VALUE': 'residential',
    'OUTPUT': 'memory:residential_only'
})
QgsProject.instance().addMapLayer(result['OUTPUT'])

# 按表达式提取
result = processing.run("native:extractbyexpression", {
    'INPUT': layer,
    'EXPRESSION': '"population" > 10000 AND "area" > 500',
    'OUTPUT': 'memory:filtered'
})
QgsProject.instance().addMapLayer(result['OUTPUT'])

# 按空间位置提取
mask_layer = QgsProject.instance().mapLayersByName("study_area")[0]
result = processing.run("native:extractbylocation", {
    'INPUT': layer,
    'PREDICATE': [0],  # 0=相交, 1=包含, 2=不相交, 3=在...内, 5=被...包含, 6=相等
    'INTERSECT': mask_layer,
    'OUTPUT': 'memory:spatial_extract'
})
QgsProject.instance().addMapLayer(result['OUTPUT'])
print(f"空间提取: {result['OUTPUT'].featureCount()} 个要素")
```

## 脚本编写规范

1. 表达式中字段名用双引号，字符串值用单引号：`"field" = 'value'`。
2. 大数据量查询必须使用 `QgsSpatialIndex` 加速空间过滤。
3. 使用 `setSubsetOfAttributes()` 限制返回字段，减少内存消耗。
4. 使用 `setLimit()` 限制返回数量，避免一次性加载过多要素。
5. 选择集操作后记得 `removeSelection()` 清除，避免影响后续操作。

## 常见陷阱

- 表达式中的 NULL 判断用 `IS NULL` 而非 `= NULL`
- `QgsFeatureRequest` 的 `setFilterRect` 和 `setFilterExpression` 可以组合使用
- `selectByExpression` 的第二个参数控制选择模式：`SetSelection`（替换）、`AddToSelection`（追加）、`RemoveFromSelection`（移除）
- `getFeatures()` 返回迭代器，只能遍历一次；需要多次使用时转为 `list()`
- `extractbylocation` 的 `PREDICATE` 是列表，可以同时指定多个空间关系
- 空间索引的 `intersects()` 基于边界框，不是精确几何相交
