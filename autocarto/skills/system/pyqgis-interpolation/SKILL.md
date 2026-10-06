---
name: pyqgis-interpolation
description: PyQGIS 空间插值脚本模板，支持 IDW、TIN 和 Kriging 插值方法
kind: workflow
---

# PyQGIS 空间插值

## 概述

本 skill 提供 PyQGIS 脚本模板，用于空间插值操作。支持反距离加权（IDW）、不规则三角网（TIN）和克里金（Kriging）插值方法。

## 核心 API 参考

| 类/方法 | 用途 |
|---------|------|
| `QgsIDWInterpolator` | IDW 插值器 |
| `QgsTinInterpolator` | TIN 插值器 |
| `QgsInterpolator.LayerData` | 插值输入数据配置 |
| `QgsGridFileWriter` | 栅格输出写入器 |
| `processing.run("qgis:idwinterpolation")` | IDW 插值算法 |
| `processing.run("qgis:tininterpolation")` | TIN 插值算法 |
| `processing.run("grass7:v.surf.rst")` | 正则化样条插值 |

## 代码模板

### 1. IDW 反距离加权插值

```python
import processing
from qgis.core import QgsProject, QgsRasterLayer

points = QgsProject.instance().mapLayersByName("sample_points")[0]

# 使用 Processing 算法
result = processing.run("qgis:idwinterpolation", {
    'INTERPOLATION_DATA': f'{points.id()}::~::0::~::0::~::0',
    # 格式: layer_id::~::attribute_index::~::input_type::~::use_z
    # attribute_index: 插值字段的索引号
    # input_type: 0=点, 1=结构线, 2=断裂线
    # use_z: 0=使用属性值, 1=使用Z值
    'DISTANCE_COEFFICIENT': 2,  # 距离幂次（通常2）
    'PIXEL_SIZE': 100,          # 输出像元大小
    'EXTENT': points.extent(),
    'OUTPUT': 'TEMPORARY_OUTPUT'
})

idw_raster = QgsRasterLayer(result['OUTPUT'], "idw_interpolation")
QgsProject.instance().addMapLayer(idw_raster)
print(f"IDW 插值完成: {idw_raster.width()}x{idw_raster.height()}")
```

### 2. IDW 插值（使用原生 API）

```python
from qgis.core import (
    QgsProject, QgsRasterLayer,
    QgsIDWInterpolator, QgsInterpolator,
    QgsGridFileWriter
)
import os

points = QgsProject.instance().mapLayersByName("sample_points")[0]

# 配置插值数据
layer_data = QgsInterpolator.LayerData()
layer_data.source = points
layer_data.interpolationAttribute = points.fields().indexOf("value")  # 插值字段索引
layer_data.sourceType = QgsInterpolator.SourcePoints
layer_data.transformContext = QgsProject.instance().transformContext()

# 创建 IDW 插值器
interpolator = QgsIDWInterpolator([layer_data])
interpolator.setDistanceCoefficient(2)

# 配置输出
extent = points.extent()
pixel_size = 100  # 像元大小
ncols = int((extent.xMaximum() - extent.xMinimum()) / pixel_size)
nrows = int((extent.yMaximum() - extent.yMinimum()) / pixel_size)

output_path = os.path.join(QgsProject.instance().homePath(), "idw_result.tif")
writer = QgsGridFileWriter(interpolator, output_path, extent, ncols, nrows)
writer.writeFile()

result_layer = QgsRasterLayer(output_path, "idw_native")
QgsProject.instance().addMapLayer(result_layer)
```

### 3. TIN 不规则三角网插值

```python
import processing
from qgis.core import QgsProject, QgsRasterLayer

points = QgsProject.instance().mapLayersByName("sample_points")[0]

result = processing.run("qgis:tininterpolation", {
    'INTERPOLATION_DATA': f'{points.id()}::~::0::~::0::~::0',
    'METHOD': 0,           # 0=线性, 1=Clough-Tocher（三次）
    'PIXEL_SIZE': 100,
    'EXTENT': points.extent(),
    'OUTPUT': 'TEMPORARY_OUTPUT'
})

tin_raster = QgsRasterLayer(result['OUTPUT'], "tin_interpolation")
QgsProject.instance().addMapLayer(tin_raster)
print(f"TIN 插值完成: {tin_raster.width()}x{tin_raster.height()}")
```

### 4. Kriging 插值（通过 GRASS）

```python
import processing
from qgis.core import QgsProject, QgsRasterLayer

points = QgsProject.instance().mapLayersByName("sample_points")[0]

# 使用 GRASS v.surf.rst（正则化样条，类似 Kriging 效果）
result = processing.run("grass7:v.surf.rst", {
    'input': points,
    'zcolumn': 'value',       # 插值字段
    'where': '',
    'mask': None,
    'tension': 40,            # 张力参数
    'smooth': None,           # 平滑参数
    'segmax': 40,
    'npmin': 300,
    'dmin': 0,
    'dmax': 0,
    'zscale': 1,
    'theta': 0,
    'scalex': 0,
    'elevation': 'TEMPORARY_OUTPUT',
    'slope': None,
    'aspect': None,
    'pcurvature': None,
    'tcurvature': None,
    'mcurvature': None,
    'GRASS_REGION_PARAMETER': None,
    'GRASS_REGION_CELLSIZE_PARAMETER': 100,
    'GRASS_RASTER_FORMAT_OPT': '',
    'GRASS_RASTER_FORMAT_META': ''
})

kriging_raster = QgsRasterLayer(result['elevation'], "kriging_interpolation")
QgsProject.instance().addMapLayer(kriging_raster)
```

### 5. 插值结果验证

```python
from qgis.core import QgsProject, QgsRasterBandStats, QgsPointXY

# 获取插值结果栅格
raster = QgsProject.instance().mapLayersByName("idw_interpolation")[0]
provider = raster.dataProvider()

# 全局统计
stats = provider.bandStatistics(1, QgsRasterBandStats.All)
print(f"插值结果统计:")
print(f"  最小值: {stats.minimumValue:.4f}")
print(f"  最大值: {stats.maximumValue:.4f}")
print(f"  均值: {stats.mean:.4f}")
print(f"  标准差: {stats.stdDev:.4f}")

# 在采样点位置验证（交叉验证思路）
points = QgsProject.instance().mapLayersByName("sample_points")[0]
errors = []
for feat in points.getFeatures():
    point = feat.geometry().asPoint()
    actual = feat["value"]
    # 从栅格采样
    result = provider.identify(QgsPointXY(point), 1)  # 1=IdentifyFormatValue
    if result.isValid():
        predicted = list(result.results().values())[0]
        if predicted is not None and actual is not None:
            errors.append(abs(predicted - actual))

if errors:
    mae = sum(errors) / len(errors)
    rmse = (sum(e**2 for e in errors) / len(errors)) ** 0.5
    print(f"  MAE: {mae:.4f}")
    print(f"  RMSE: {rmse:.4f}")
    print(f"  验证点数: {len(errors)}")
```

## 脚本编写规范

1. `INTERPOLATION_DATA` 格式为 `'layer_id::~::attr_index::~::input_type::~::use_z'`，注意字段索引从 0 开始。
2. `PIXEL_SIZE` 应根据采样点密度合理设置，过小会导致计算量大且过拟合。
3. 插值范围默认使用采样点的 extent，可手动扩展以避免边缘效应。
4. 插值完成后用统计信息验证结果值域是否合理。
5. IDW 的距离幂次通常为 2，增大幂次会使插值更局部化。

## 常见陷阱

- `INTERPOLATION_DATA` 的 `attribute_index` 是字段在图层中的位置索引，不是字段名
- IDW 在采样点稀疏区域会产生"牛眼"效应（同心圆状伪影）
- TIN 插值在凸包外部无法外推，结果为 NoData
- GRASS 插值工具需要 GRASS GIS 已安装
- 插值前应移除异常值/离群点，否则会严重影响结果
- 像元大小设置过小（如 1m）在大范围区域会生成巨大栅格文件
