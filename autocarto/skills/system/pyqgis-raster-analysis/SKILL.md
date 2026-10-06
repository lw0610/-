---
name: pyqgis-raster-analysis
description: PyQGIS 栅格分析脚本模板，支持栅格计算、裁剪、合并、重分类和分区统计
kind: workflow
---

# PyQGIS 栅格分析

## 概述

本 skill 提供 PyQGIS 脚本模板，用于栅格数据的计算、裁剪、合并、重采样、重分类和分区统计。通过 Processing 算法和 GDAL 工具实现栅格操作。

## 核心 API 参考

| 类/方法 | 用途 |
|---------|------|
| `QgsRasterLayer(uri, name)` | 创建栅格图层 |
| `layer.bandCount()` | 波段数 |
| `layer.width() / height()` | 像元列数/行数 |
| `layer.rasterUnitsPerPixelX()` | X 方向分辨率 |
| `layer.dataProvider().block()` | 读取栅格数据块 |
| `processing.run("native:rastercalculator")` | 栅格计算器 |
| `processing.run("gdal:cliprasterbyextent")` | 按范围裁剪 |
| `processing.run("gdal:cliprasterbymask")` | 按掩膜裁剪 |
| `processing.run("gdal:merge")` | 栅格合并 |
| `processing.run("gdal:warpreproject")` | 重投影/重采样 |

## 代码模板

### 1. NDVI 植被指数计算

```python
import processing
from qgis.core import QgsProject, QgsRasterLayer

# 假设已加载多光谱影像，红波段=Band3，近红外=Band4
raster = QgsProject.instance().mapLayersByName("multispectral")[0]

# 使用栅格计算器
result = processing.run("native:rastercalculator", {
    'EXPRESSION': '("multispectral@4" - "multispectral@3") / ("multispectral@4" + "multispectral@3")',
    'LAYERS': [raster],
    'CELLSIZE': 0,  # 0=自动
    'EXTENT': None,  # None=自动
    'CRS': None,
    'OUTPUT': 'TEMPORARY_OUTPUT'
})

ndvi_layer = QgsRasterLayer(result['OUTPUT'], "NDVI")
QgsProject.instance().addMapLayer(ndvi_layer)
print(f"NDVI 计算完成，范围: {ndvi_layer.extent().toString()}")
```

### 2. 栅格裁剪

```python
import processing
from qgis.core import QgsProject

raster = QgsProject.instance().mapLayersByName("dem")[0]

# 按范围裁剪
result_extent = processing.run("gdal:cliprasterbyextent", {
    'INPUT': raster,
    'PROJWIN': '116.0,117.0,39.0,40.0',  # xmin,xmax,ymin,ymax
    'NODATA': -9999,
    'DATA_TYPE': 0,  # 0=使用输入类型
    'OUTPUT': 'TEMPORARY_OUTPUT'
})
from qgis.core import QgsRasterLayer
clipped = QgsRasterLayer(result_extent['OUTPUT'], "dem_clipped_extent")
QgsProject.instance().addMapLayer(clipped)

# 按矢量掩膜裁剪
mask_layer = QgsProject.instance().mapLayersByName("study_area")[0]
result_mask = processing.run("gdal:cliprasterbymask", {
    'INPUT': raster,
    'MASK': mask_layer,
    'SOURCE_CRS': None,
    'TARGET_CRS': None,
    'NODATA': -9999,
    'ALPHA_BAND': False,
    'CROP_TO_CUTLINE': True,
    'KEEP_RESOLUTION': True,
    'OUTPUT': 'TEMPORARY_OUTPUT'
})
masked = QgsRasterLayer(result_mask['OUTPUT'], "dem_clipped_mask")
QgsProject.instance().addMapLayer(masked)
```

### 3. 栅格合并

```python
import processing
from qgis.core import QgsProject, QgsRasterLayer

# 获取多个栅格图层
raster_names = ["dem_tile1", "dem_tile2", "dem_tile3"]
rasters = [QgsProject.instance().mapLayersByName(n)[0] for n in raster_names]

result = processing.run("gdal:merge", {
    'INPUT': rasters,
    'PCT': False,
    'SEPARATE': False,  # False=合并为单波段, True=每个输入一个波段
    'NODATA_INPUT': -9999,
    'NODATA_OUTPUT': -9999,
    'DATA_TYPE': 5,  # 5=Float32
    'OUTPUT': 'TEMPORARY_OUTPUT'
})

merged = QgsRasterLayer(result['OUTPUT'], "dem_merged")
QgsProject.instance().addMapLayer(merged)
print(f"合并完成: {merged.width()}x{merged.height()}, 波段数: {merged.bandCount()}")
```

### 4. 栅格重分类

```python
import processing
from qgis.core import QgsProject, QgsRasterLayer

raster = QgsProject.instance().mapLayersByName("slope")[0]

# 重分类表: [最小值, 最大值, 新值]
reclass_table = [
    0, 5, 1,      # 0-5度 → 1（平坦）
    5, 15, 2,     # 5-15度 → 2（缓坡）
    15, 25, 3,    # 15-25度 → 3（中坡）
    25, 45, 4,    # 25-45度 → 4（陡坡）
    45, 90, 5     # 45-90度 → 5（极陡）
]

result = processing.run("native:reclassifybytable", {
    'INPUT_RASTER': raster,
    'RASTER_BAND': 1,
    'TABLE': reclass_table,
    'NO_DATA': -9999,
    'RANGE_BOUNDARIES': 0,  # 0=min<=v<max, 1=min<v<=max
    'NODATA_FOR_MISSING': True,
    'DATA_TYPE': 5,
    'OUTPUT': 'TEMPORARY_OUTPUT'
})

reclassed = QgsRasterLayer(result['OUTPUT'], "slope_classes")
QgsProject.instance().addMapLayer(reclassed)
```

### 5. 分区统计

```python
import processing
from qgis.core import QgsProject

zones = QgsProject.instance().mapLayersByName("districts")[0]
raster = QgsProject.instance().mapLayersByName("population_density")[0]

result = processing.run("native:zonalstatisticsfb", {
    'INPUT': zones,
    'INPUT_RASTER': raster,
    'RASTER_BAND': 1,
    'COLUMN_PREFIX': 'pop_',
    'STATISTICS': [0, 1, 2, 3, 4, 5, 6],  # 0=count,1=sum,2=mean,3=median,4=stdev,5=min,6=max
    'OUTPUT': 'memory:zonal_stats'
})

stats_layer = result['OUTPUT']
QgsProject.instance().addMapLayer(stats_layer)

for feat in stats_layer.getFeatures():
    print(f"{feat['name']}: 均值={feat['pop_mean']:.2f}, 总和={feat['pop_sum']:.0f}")
```

### 6. 栅格统计信息

```python
from qgis.core import QgsProject, QgsRasterBandStats

raster = QgsProject.instance().mapLayersByName("dem")[0]
provider = raster.dataProvider()

for band in range(1, raster.bandCount() + 1):
    stats = provider.bandStatistics(band, QgsRasterBandStats.All)
    print(f"波段 {band}:")
    print(f"  最小值: {stats.minimumValue:.4f}")
    print(f"  最大值: {stats.maximumValue:.4f}")
    print(f"  均值: {stats.mean:.4f}")
    print(f"  标准差: {stats.stdDev:.4f}")
    print(f"  有效像元数: {stats.elementCount}")
```

## 脚本编写规范

1. 栅格计算器表达式中图层引用格式为 `"layer_name@band_number"`。
2. 裁剪和合并操作指定 `NODATA` 值，避免无数据区域被错误处理。
3. 多栅格运算前确认分辨率和范围一致，不一致时先用 `gdal:warpreproject` 统一。
4. 重分类表为一维列表，每 3 个值一组：`[min, max, new_value, ...]`。
5. 栅格输出使用 `'TEMPORARY_OUTPUT'`，然后用 `QgsRasterLayer` 加载结果。

## 常见陷阱

- 栅格计算器表达式中的图层名必须与 QGIS 中显示的名称完全一致
- `gdal:merge` 的 `SEPARATE=True` 会将每个输入栅格作为独立波段，通常应设为 False
- NoData 值在计算中会传播，`-9999 + 100 = -9999`，需在表达式中显式处理
- 分区统计要求矢量和栅格 CRS 一致，否则结果不准确
- `'TEMPORARY_OUTPUT'` 生成的文件在 QGIS 关闭后会被清理，需要持久化时指定输出路径
- 大栅格（>1GB）操作可能耗时较长，建议先裁剪到研究区域再分析
