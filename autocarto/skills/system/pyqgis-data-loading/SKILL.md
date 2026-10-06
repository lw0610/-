---
name: pyqgis-data-loading
description: PyQGIS 数据加载与质量检查脚本模板，支持矢量/栅格/远程数据源的脚本化加载和验证
kind: workflow
---

# PyQGIS 数据加载与质量检查

## 概述

本 skill 提供 PyQGIS 脚本模板，用于数据加载、格式识别和质量检查。所有操作通过编写 PyQGIS 脚本完成，不依赖预注册的 Processing 工具。

## 核心 API 参考

| 类/方法 | 用途 |
|---------|------|
| `QgsVectorLayer(uri, name, provider)` | 创建矢量图层 |
| `QgsRasterLayer(uri, name, provider)` | 创建栅格图层 |
| `QgsProject.instance().addMapLayer(layer)` | 添加图层到工程 |
| `layer.isValid()` | 检查图层有效性 |
| `layer.featureCount()` | 获取要素数量 |
| `layer.crs()` | 获取坐标参考系 |
| `layer.extent()` | 获取空间范围 |
| `layer.fields()` | 获取字段列表 |
| `QgsVectorFileWriter` | 矢量数据写出 |

## 代码模板

### 1. 加载本地矢量数据（SHP/GeoJSON/GPKG）

```python
from qgis.core import QgsVectorLayer, QgsProject

# SHP
layer = QgsVectorLayer("/path/to/data.shp", "my_layer", "ogr")

# GeoJSON
layer = QgsVectorLayer("/path/to/data.geojson", "my_layer", "ogr")

# GPKG（指定图层名）
layer = QgsVectorLayer("/path/to/data.gpkg|layername=target_layer", "my_layer", "ogr")

if not layer.isValid():
    raise Exception(f"图层加载失败: {layer.error().message()}")

QgsProject.instance().addMapLayer(layer)
```

### 2. 加载 CSV 带坐标列

```python
from qgis.core import QgsVectorLayer, QgsProject

uri = (
    "file:///path/to/data.csv"
    "?delimiter=,"
    "&xField=longitude"
    "&yField=latitude"
    "&crs=EPSG:4326"
    "&encoding=UTF-8"
)
layer = QgsVectorLayer(uri, "csv_points", "delimitedtext")

if not layer.isValid():
    raise Exception("CSV 加载失败，请检查字段名和分隔符")

QgsProject.instance().addMapLayer(layer)
```

### 3. 加载栅格数据（GeoTIFF）

```python
from qgis.core import QgsRasterLayer, QgsProject

layer = QgsRasterLayer("/path/to/dem.tif", "dem_layer")

if not layer.isValid():
    raise Exception("栅格加载失败")

# 检查栅格元数据
print(f"波段数: {layer.bandCount()}")
print(f"宽x高: {layer.width()} x {layer.height()}")
print(f"CRS: {layer.crs().authid()}")
print(f"范围: {layer.extent().toString()}")

QgsProject.instance().addMapLayer(layer)
```

### 4. 加载 WFS/WMS 远程数据源

```python
from qgis.core import QgsVectorLayer, QgsRasterLayer, QgsProject

# WFS
wfs_uri = "pagingEnabled='true' url='https://example.com/wfs' typename='namespace:layername'"
wfs_layer = QgsVectorLayer(wfs_uri, "wfs_layer", "WFS")

# WMS
wms_uri = (
    "crs=EPSG:4326"
    "&format=image/png"
    "&layers=layer_name"
    "&styles="
    "&url=https://example.com/wms"
)
wms_layer = QgsRasterLayer(wms_uri, "wms_layer", "wms")

if wfs_layer.isValid():
    QgsProject.instance().addMapLayer(wfs_layer)
if wms_layer.isValid():
    QgsProject.instance().addMapLayer(wms_layer)
```

### 5. 批量加载目录下所有文件

```python
import os
from qgis.core import QgsVectorLayer, QgsRasterLayer, QgsProject

data_dir = "/path/to/data_folder"
vector_exts = {'.shp', '.geojson', '.gpkg', '.kml'}
raster_exts = {'.tif', '.tiff', '.img', '.asc'}

for filename in os.listdir(data_dir):
    filepath = os.path.join(data_dir, filename)
    ext = os.path.splitext(filename)[1].lower()
    name = os.path.splitext(filename)[0]

    if ext in vector_exts:
        layer = QgsVectorLayer(filepath, name, "ogr")
    elif ext in raster_exts:
        layer = QgsRasterLayer(filepath, name)
    else:
        continue

    if layer.isValid():
        QgsProject.instance().addMapLayer(layer)
    else:
        print(f"跳过无效文件: {filename}")
```

### 6. 数据质量检查脚本

```python
from qgis.core import QgsProject, QgsGeometry

layer = QgsProject.instance().mapLayersByName("target_layer")[0]

# 基本信息
total = layer.featureCount()
crs = layer.crs().authid()
extent = layer.extent()
fields = [f.name() for f in layer.fields()]

# 几何有效性检查
invalid_count = 0
null_geom_count = 0
for feat in layer.getFeatures():
    geom = feat.geometry()
    if geom.isNull():
        null_geom_count += 1
    elif not geom.isGeosValid():
        invalid_count += 1

# 空值统计
null_stats = {}
for field in layer.fields():
    null_count = 0
    for feat in layer.getFeatures():
        if feat[field.name()] is None or feat[field.name()] == NULL:
            null_count += 1
    if null_count > 0:
        null_stats[field.name()] = null_count

# 输出报告
report = f"""
=== 数据质量报告 ===
图层名: {layer.name()}
要素总数: {total}
CRS: {crs}
范围: {extent.toString()}
字段: {', '.join(fields)}
无效几何: {invalid_count} ({invalid_count/total*100:.1f}%)
空几何: {null_geom_count}
空值字段: {null_stats}
"""
print(report)
```

## 脚本编写规范

1. 加载后必须检查 `layer.isValid()`，无效时抛出异常并附带错误信息。
2. 使用 `QgsProject.instance().addMapLayer()` 将图层添加到工程，确保用户可见。
3. 文件路径使用绝对路径，避免相对路径导致的定位问题。
4. CSV 加载时必须明确指定 `xField`、`yField`、`crs` 和 `delimiter`。
5. 批量加载时跳过无效文件并记录日志，不中断整体流程。

## 常见陷阱

- SHP 文件缺少 `.prj` 文件时，图层 CRS 为空，需手动设置：`layer.setCrs(QgsCoordinateReferenceSystem("EPSG:4326"))`
- CSV 中文编码问题：URI 中添加 `&encoding=GBK` 或 `&encoding=UTF-8`
- GPKG 包含多个图层时，不指定 `layername` 会加载第一个图层
- `QgsProject.instance().addMapLayer(layer, True)` 第二个参数控制是否添加到图层树，默认 True
- 远程数据源加载可能超时，建议先用 `file_downloader` 下载到本地
