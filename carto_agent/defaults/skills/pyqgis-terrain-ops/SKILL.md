---
name: pyqgis-terrain-ops
description: PyQGIS 地形分析脚本模板，支持 DEM 衍生分析（坡度/坡向/山体阴影）、等高线生成和流域分析
kind: workflow
---

# PyQGIS 地形分析

## 概述

本 skill 提供 PyQGIS 脚本模板，用于 DEM 地形衍生分析。涵盖坡度、坡向、山体阴影、粗糙度计算，等高线生成和流域分析。

## 核心 API 参考

| 类/方法 | 用途 |
|---------|------|
| `processing.run("native:slope")` | 坡度计算 |
| `processing.run("native:aspect")` | 坡向计算 |
| `processing.run("native:hillshade")` | 山体阴影 |
| `processing.run("native:ruggednessindex")` | 粗糙度指数 |
| `processing.run("gdal:slope")` | GDAL 坡度 |
| `processing.run("gdal:aspect")` | GDAL 坡向 |
| `processing.run("gdal:hillshade")` | GDAL 山体阴影 |
| `processing.run("gdal:contour")` | 等高线生成 |
| `processing.run("grass7:r.watershed")` | 流域分析 |

## 代码模板

### 1. 完整地形分析流水线

```python
import processing
from qgis.core import QgsProject, QgsRasterLayer

dem = QgsProject.instance().mapLayersByName("dem")[0]

# 坡度（度）
slope_result = processing.run("native:slope", {
    'INPUT': dem,
    'Z_FACTOR': 1,
    'OUTPUT': 'TEMPORARY_OUTPUT'
})
slope = QgsRasterLayer(slope_result['OUTPUT'], "slope_degrees")
QgsProject.instance().addMapLayer(slope)

# 坡向（0-360度，北=0）
aspect_result = processing.run("native:aspect", {
    'INPUT': dem,
    'Z_FACTOR': 1,
    'OUTPUT': 'TEMPORARY_OUTPUT'
})
aspect = QgsRasterLayer(aspect_result['OUTPUT'], "aspect")
QgsProject.instance().addMapLayer(aspect)

# 山体阴影
hillshade_result = processing.run("native:hillshade", {
    'INPUT': dem,
    'Z_FACTOR': 1,
    'AZIMUTH': 315,      # 光源方位角（度）
    'V_ANGLE': 45,       # 光源高度角（度）
    'OUTPUT': 'TEMPORARY_OUTPUT'
})
hillshade = QgsRasterLayer(hillshade_result['OUTPUT'], "hillshade")
QgsProject.instance().addMapLayer(hillshade)

# 粗糙度指数
ruggedness_result = processing.run("native:ruggednessindex", {
    'INPUT': dem,
    'Z_FACTOR': 1,
    'OUTPUT': 'TEMPORARY_OUTPUT'
})
ruggedness = QgsRasterLayer(ruggedness_result['OUTPUT'], "ruggedness")
QgsProject.instance().addMapLayer(ruggedness)

print("地形分析完成: 坡度、坡向、山体阴影、粗糙度")
```

### 2. 使用 GDAL 地形分析（更多参数控制）

```python
import processing
from qgis.core import QgsProject, QgsRasterLayer

dem = QgsProject.instance().mapLayersByName("dem")[0]

# GDAL 坡度（支持百分比输出）
slope_result = processing.run("gdal:slope", {
    'INPUT': dem,
    'BAND': 1,
    'SCALE': 1,           # 垂直单位与水平单位的比值
    'AS_PERCENT': False,   # True=百分比, False=度
    'COMPUTE_EDGES': True, # 计算边缘像元
    'ZEVENBERGEN': False,  # True=Zevenbergen-Thorne算法
    'OUTPUT': 'TEMPORARY_OUTPUT'
})
slope = QgsRasterLayer(slope_result['OUTPUT'], "slope_gdal")
QgsProject.instance().addMapLayer(slope)

# GDAL 山体阴影（多方向）
hillshade_result = processing.run("gdal:hillshade", {
    'INPUT': dem,
    'BAND': 1,
    'Z_FACTOR': 1,
    'SCALE': 1,
    'AZIMUTH': 315,
    'ALTITUDE': 45,
    'COMPUTE_EDGES': True,
    'ZEVENBERGEN': False,
    'COMBINED': False,     # True=多方向组合
    'MULTIDIRECTIONAL': True,  # 多方向光照
    'OUTPUT': 'TEMPORARY_OUTPUT'
})
hillshade = QgsRasterLayer(hillshade_result['OUTPUT'], "hillshade_multi")
QgsProject.instance().addMapLayer(hillshade)
```

### 3. 等高线生成

```python
import processing
from qgis.core import QgsProject, QgsRasterLayer

dem = QgsProject.instance().mapLayersByName("dem")[0]

# 基本等高线
result = processing.run("gdal:contour", {
    'INPUT': dem,
    'BAND': 1,
    'INTERVAL': 50,        # 等高距（米）
    'FIELD_NAME': 'ELEV',  # 高程字段名
    'CREATE_3D': False,
    'IGNORE_NODATA': True,
    'NODATA': -9999,
    'OFFSET': 0,
    'OUTPUT': 'TEMPORARY_OUTPUT'
})

from qgis.core import QgsVectorLayer
contours = QgsVectorLayer(result['OUTPUT'], "contours_50m", "ogr")
QgsProject.instance().addMapLayer(contours)
print(f"等高线生成完成: {contours.featureCount()} 条")

# 生成计曲线和首曲线
# 计曲线（每 250m）
result_index = processing.run("gdal:contour", {
    'INPUT': dem,
    'BAND': 1,
    'INTERVAL': 250,
    'FIELD_NAME': 'ELEV',
    'OUTPUT': 'TEMPORARY_OUTPUT'
})
index_contours = QgsVectorLayer(result_index['OUTPUT'], "index_contours_250m", "ogr")
QgsProject.instance().addMapLayer(index_contours)
```

### 4. 流域分析（GRASS r.watershed）

```python
import processing
from qgis.core import QgsProject, QgsRasterLayer

dem = QgsProject.instance().mapLayersByName("dem")[0]

result = processing.run("grass7:r.watershed", {
    'elevation': dem,
    'depression': None,
    'flow': None,
    'disturbed_land': None,
    'blocking': None,
    'threshold': 10000,     # 最小汇水面积（像元数）
    'max_slope_length': 0,
    'convergence': 5,
    '-s': True,             # SFD 单流向
    '-m': False,
    '-4': False,
    '-a': False,
    '-b': False,
    'accumulation': 'TEMPORARY_OUTPUT',
    'drainage': 'TEMPORARY_OUTPUT',
    'basin': 'TEMPORARY_OUTPUT',
    'stream': 'TEMPORARY_OUTPUT',
    'half_basin': None,
    'length_slope': None,
    'slope_steepness': None,
    'tci': None,
    'spi': None,
    'GRASS_REGION_PARAMETER': None,
    'GRASS_REGION_CELLSIZE_PARAMETER': 0,
    'GRASS_RASTER_FORMAT_OPT': '',
    'GRASS_RASTER_FORMAT_META': ''
})

# 加载结果
accumulation = QgsRasterLayer(result['accumulation'], "flow_accumulation")
drainage = QgsRasterLayer(result['drainage'], "flow_direction")
basin = QgsRasterLayer(result['basin'], "watersheds")
stream = QgsRasterLayer(result['stream'], "stream_network")

for lyr in [accumulation, drainage, basin, stream]:
    if lyr.isValid():
        QgsProject.instance().addMapLayer(lyr)

print("流域分析完成: 汇流累积、流向、流域、河网")
```

### 5. 视域分析

```python
import processing
from qgis.core import QgsProject, QgsRasterLayer

dem = QgsProject.instance().mapLayersByName("dem")[0]
viewpoints = QgsProject.instance().mapLayersByName("observation_points")[0]

result = processing.run("native:viewshed", {
    'INPUT': dem,
    'BAND': 1,
    'OBSERVER_POINTS': viewpoints,
    'OBSERVER_HEIGHT': 1.7,    # 观察者高度（米）
    'TARGET_HEIGHT': 0,        # 目标高度（米）
    'MAX_DISTANCE': 10000,     # 最大分析距离（米）
    'OUTPUT': 'TEMPORARY_OUTPUT'
})

viewshed = QgsRasterLayer(result['OUTPUT'], "viewshed")
QgsProject.instance().addMapLayer(viewshed)
print("视域分析完成")
```

## 脚本编写规范

1. 地形分析要求 DEM 使用投影坐标系（米），地理坐标系下坡度计算不准确。
2. `Z_FACTOR` 用于垂直夸张或单位转换：水平单位为米、高程为米时设为 1；水平为度时需设为 111120。
3. 等高线生成前检查 DEM 值域，确定合理的等高距。
4. 流域分析的 `threshold` 参数影响河网密度，值越小河网越密。
5. 大面积 DEM 分析耗时较长，建议先裁剪到研究区域。

## 常见陷阱

- 地理坐标系（度）下直接计算坡度会得到极小值，必须先重投影或设置正确的 `SCALE` 参数
- GDAL 和 QGIS 原生算法的参数名不同（如 `Z_FACTOR` vs `SCALE`），注意区分
- 山体阴影的 `AZIMUTH` 是光源方位角（从北顺时针），不是阴影方向
- `grass7:r.watershed` 需要 GRASS GIS 已安装，否则会报错
- 等高线在平坦区域可能产生大量碎片线段，建议后处理时简化或过滤短线段
- 视域分析的 `MAX_DISTANCE` 过大会显著增加计算时间
