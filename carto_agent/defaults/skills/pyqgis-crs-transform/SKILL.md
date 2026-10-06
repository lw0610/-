---
name: pyqgis-crs-transform
description: PyQGIS 坐标参考系诊断与转换脚本模板，支持 CRS 查询、坐标转换和批量重投影
kind: workflow
---

# PyQGIS 坐标参考系诊断与转换

## 概述

本 skill 提供 PyQGIS 脚本模板，用于坐标参考系的查询、诊断、转换和批量重投影。涵盖 CRS 创建、坐标点转换、图层重投影等常见操作。

## 核心 API 参考

| 类/方法 | 用途 |
|---------|------|
| `QgsCoordinateReferenceSystem(authid)` | 创建 CRS 对象 |
| `QgsCoordinateTransform(src, dst, project)` | 创建坐标转换器 |
| `transform.transform(point)` | 转换单个坐标点 |
| `transform.transformBoundingBox(rect)` | 转换矩形范围 |
| `layer.setCrs(crs)` | 设置图层 CRS |
| `processing.run("native:reprojectlayer")` | 图层重投影 |
| `crs.isValid()` | 检查 CRS 有效性 |
| `crs.authid()` | 获取 EPSG 代码 |
| `crs.description()` | 获取 CRS 描述 |

## 代码模板

### 1. CRS 查询与诊断

```python
from qgis.core import QgsProject, QgsCoordinateReferenceSystem

# 检查所有图层的 CRS
project = QgsProject.instance()
crs_report = {}

for layer_id, layer in project.mapLayers().items():
    crs = layer.crs()
    crs_info = {
        "authid": crs.authid(),
        "description": crs.description(),
        "is_geographic": crs.isGeographic(),
        "map_units": str(crs.mapUnits()),
        "is_valid": crs.isValid()
    }
    crs_report[layer.name()] = crs_info

# 检测 CRS 不一致
unique_crs = set(info["authid"] for info in crs_report.values())
if len(unique_crs) > 1:
    print(f"警告: 检测到 {len(unique_crs)} 种不同的 CRS: {unique_crs}")
    print("建议统一投影后再进行空间分析")
else:
    print(f"所有图层 CRS 一致: {unique_crs.pop()}")

for name, info in crs_report.items():
    print(f"  {name}: {info['authid']} - {info['description']}")
```

### 2. 单点坐标转换

```python
from qgis.core import (
    QgsCoordinateReferenceSystem,
    QgsCoordinateTransform,
    QgsProject,
    QgsPointXY
)

src_crs = QgsCoordinateReferenceSystem("EPSG:4326")   # WGS84 经纬度
dst_crs = QgsCoordinateReferenceSystem("EPSG:32650")  # UTM Zone 50N

transform = QgsCoordinateTransform(src_crs, dst_crs, QgsProject.instance())

# 转换单个点
point_wgs84 = QgsPointXY(116.397, 39.908)  # 北京天安门
point_utm = transform.transform(point_wgs84)
print(f"WGS84: {point_wgs84} -> UTM: {point_utm}")

# 反向转换
reverse_transform = QgsCoordinateTransform(dst_crs, src_crs, QgsProject.instance())
point_back = reverse_transform.transform(point_utm)
print(f"UTM: {point_utm} -> WGS84: {point_back}")
```

### 3. 图层重投影（通过 Processing）

```python
import processing
from qgis.core import QgsProject

layer = QgsProject.instance().mapLayersByName("input_layer")[0]

result = processing.run("native:reprojectlayer", {
    'INPUT': layer,
    'TARGET_CRS': 'EPSG:32650',
    'OUTPUT': 'memory:reprojected_layer'
})

reprojected = result['OUTPUT']
QgsProject.instance().addMapLayer(reprojected)
print(f"重投影完成: {reprojected.crs().authid()}, 要素数: {reprojected.featureCount()}")
```

### 4. 批量统一 CRS

```python
import processing
from qgis.core import QgsProject

target_crs = "EPSG:4326"
project = QgsProject.instance()

for layer_id, layer in project.mapLayers().items():
    if layer.crs().authid() != target_crs:
        print(f"重投影: {layer.name()} ({layer.crs().authid()} -> {target_crs})")

        if layer.type() == layer.VectorLayer:
            result = processing.run("native:reprojectlayer", {
                'INPUT': layer,
                'TARGET_CRS': target_crs,
                'OUTPUT': 'memory:' + layer.name() + '_reprojected'
            })
            QgsProject.instance().addMapLayer(result['OUTPUT'])
        elif layer.type() == layer.RasterLayer:
            result = processing.run("gdal:warpreproject", {
                'INPUT': layer,
                'TARGET_CRS': target_crs,
                'RESAMPLING': 0,  # 最近邻
                'OUTPUT': 'TEMPORARY_OUTPUT'
            })
            from qgis.core import QgsRasterLayer
            raster_out = QgsRasterLayer(result['OUTPUT'], layer.name() + '_reprojected')
            QgsProject.instance().addMapLayer(raster_out)

print("批量重投影完成")
```

### 5. 自定义投影定义

```python
from qgis.core import QgsCoordinateReferenceSystem

# 从 EPSG 代码创建
crs_epsg = QgsCoordinateReferenceSystem("EPSG:4490")  # CGCS2000

# 从 Proj4 字符串创建
crs_proj4 = QgsCoordinateReferenceSystem()
crs_proj4.createFromProj4("+proj=tmerc +lat_0=0 +lon_0=117 +k=1 +x_0=500000 +y_0=0 +ellps=GRS80 +units=m +no_defs")

# 从 WKT 创建
crs_wkt = QgsCoordinateReferenceSystem()
crs_wkt.createFromWkt('GEOGCS["GCS_China_Geodetic_Coordinate_System_2000"...]')

# 为无 CRS 的图层设置投影
layer = QgsProject.instance().mapLayersByName("no_crs_layer")[0]
layer.setCrs(QgsCoordinateReferenceSystem("EPSG:4326"))
print(f"已设置 CRS: {layer.crs().authid()}")
```

## 脚本编写规范

1. 坐标转换前必须检查源和目标 CRS 的有效性：`crs.isValid()`。
2. 使用 `QgsCoordinateTransform` 时必须传入 `QgsProject.instance()` 作为上下文。
3. 批量重投影时，矢量用 `native:reprojectlayer`，栅格用 `gdal:warpreproject`。
4. 重投影后验证要素数量和空间范围是否合理。
5. 地理坐标系（度）和投影坐标系（米）的距离计算结果差异巨大，分析前务必确认。

## 常见陷阱

- `layer.setCrs()` 只修改 CRS 元数据，不转换坐标值；真正的坐标转换需要 `reprojectlayer`
- 中国常用 CRS：EPSG:4326（WGS84）、EPSG:4490（CGCS2000）、EPSG:32649-32651（UTM）
- 栅格重投影的重采样方法影响结果：连续数据用双线性（1），分类数据用最近邻（0）
- `QgsCoordinateTransform` 在跨半球转换时可能出现精度问题，建议分块处理
- EPSG:4326 和 EPSG:4490 在中国区域差异极小（厘米级），但不应混用
