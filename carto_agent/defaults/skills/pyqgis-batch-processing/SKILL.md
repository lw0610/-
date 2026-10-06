---
name: pyqgis-batch-processing
description: PyQGIS 批量处理脚本模板，支持批量文件加载、批量格式转换、批量裁剪和目录遍历处理
kind: workflow
---

# PyQGIS 批量处理

## 概述

本 skill 提供 PyQGIS 脚本模板，用于批量数据处理操作。涵盖批量文件加载、批量格式转换、批量裁剪、批量导出和目录遍历处理模式。

## 核心 API 参考

| 类/方法 | 用途 |
|---------|------|
| `os.listdir() / os.walk()` | 目录遍历 |
| `glob.glob(pattern)` | 文件模式匹配 |
| `processing.run(algorithm, params)` | 执行 Processing 算法 |
| `QgsVectorFileWriter` | 矢量数据写出 |
| `QgsRasterFileWriter` | 栅格数据写出 |
| `QgsProject.instance()` | 工程实例 |
| `QgsProcessingFeedback` | 处理进度反馈 |

## 代码模板

### 1. 批量格式转换（SHP → GPKG）

```python
import os
import processing
from qgis.core import QgsVectorLayer, QgsProject

input_dir = "/path/to/shp_files"
output_dir = "/path/to/output"
os.makedirs(output_dir, exist_ok=True)

shp_files = [f for f in os.listdir(input_dir) if f.endswith('.shp')]
success_count = 0
fail_count = 0

for shp_file in shp_files:
    input_path = os.path.join(input_dir, shp_file)
    output_name = os.path.splitext(shp_file)[0] + ".gpkg"
    output_path = os.path.join(output_dir, output_name)

    try:
        layer = QgsVectorLayer(input_path, "temp", "ogr")
        if not layer.isValid():
            print(f"跳过无效文件: {shp_file}")
            fail_count += 1
            continue

        result = processing.run("native:savefeatures", {
            'INPUT': layer,
            'OUTPUT': output_path
        })
        success_count += 1
        print(f"转换成功: {shp_file} -> {output_name}")
    except Exception as e:
        print(f"转换失败: {shp_file}, 错误: {e}")
        fail_count += 1

print(f"\n批量转换完成: 成功 {success_count}, 失败 {fail_count}")
```

### 2. 批量裁剪（矢量）

```python
import os
import processing
from qgis.core import QgsVectorLayer, QgsProject

# 裁剪掩膜
mask_layer = QgsProject.instance().mapLayersByName("study_area")[0]

# 待裁剪的图层列表
layer_names = ["roads", "buildings", "landuse", "rivers"]
output_dir = "/path/to/clipped"
os.makedirs(output_dir, exist_ok=True)

for name in layer_names:
    layers = QgsProject.instance().mapLayersByName(name)
    if not layers:
        print(f"图层不存在: {name}")
        continue

    layer = layers[0]
    output_path = os.path.join(output_dir, f"{name}_clipped.gpkg")

    try:
        result = processing.run("native:clip", {
            'INPUT': layer,
            'OVERLAY': mask_layer,
            'OUTPUT': output_path
        })
        clipped = QgsVectorLayer(output_path, f"{name}_clipped", "ogr")
        QgsProject.instance().addMapLayer(clipped)
        print(f"裁剪完成: {name} -> {clipped.featureCount()} 个要素")
    except Exception as e:
        print(f"裁剪失败: {name}, 错误: {e}")
```

### 3. 批量栅格裁剪

```python
import os
import glob
import processing
from qgis.core import QgsRasterLayer, QgsProject

mask_layer = QgsProject.instance().mapLayersByName("study_area")[0]
input_dir = "/path/to/rasters"
output_dir = "/path/to/clipped_rasters"
os.makedirs(output_dir, exist_ok=True)

tif_files = glob.glob(os.path.join(input_dir, "*.tif"))

for tif_path in tif_files:
    name = os.path.splitext(os.path.basename(tif_path))[0]
    output_path = os.path.join(output_dir, f"{name}_clipped.tif")

    try:
        raster = QgsRasterLayer(tif_path, "temp")
        if not raster.isValid():
            print(f"跳过无效栅格: {tif_path}")
            continue

        result = processing.run("gdal:cliprasterbymask", {
            'INPUT': raster,
            'MASK': mask_layer,
            'NODATA': -9999,
            'CROP_TO_CUTLINE': True,
            'KEEP_RESOLUTION': True,
            'OUTPUT': output_path
        })
        print(f"裁剪完成: {name}")
    except Exception as e:
        print(f"裁剪失败: {name}, 错误: {e}")
```

### 4. 批量导出地图（多图层分别导出 PNG）

```python
import os
from qgis.core import (
    QgsProject, QgsPrintLayout, QgsLayoutItemMap,
    QgsLayoutSize, QgsLayoutPoint, QgsUnitTypes, QgsLayoutExporter
)

project = QgsProject.instance()
output_dir = "/path/to/maps"
os.makedirs(output_dir, exist_ok=True)

# 要导出的图层列表
layer_names = ["district_A", "district_B", "district_C"]

for name in layer_names:
    layers = project.mapLayersByName(name)
    if not layers:
        continue

    layer = layers[0]

    # 创建临时布局
    layout = QgsPrintLayout(project)
    layout.initializeDefaults()
    layout.setName(f"temp_{name}")

    page = layout.pageCollection().page(0)
    page.setPageSize(QgsLayoutSize(297, 210, QgsUnitTypes.LayoutMillimeters))

    map_item = QgsLayoutItemMap(layout)
    map_item.setRect(0, 0, 280, 190)
    map_item.attemptMove(QgsLayoutPoint(8, 10, QgsUnitTypes.LayoutMillimeters))
    map_item.attemptResize(QgsLayoutSize(280, 190, QgsUnitTypes.LayoutMillimeters))
    map_item.zoomToExtent(layer.extent())
    layout.addLayoutItem(map_item)

    # 导出
    exporter = QgsLayoutExporter(layout)
    png_path = os.path.join(output_dir, f"{name}.png")
    settings = QgsLayoutExporter.ImageExportSettings()
    settings.dpi = 150

    result = exporter.exportToImage(png_path, settings)
    if result == QgsLayoutExporter.Success:
        print(f"导出成功: {png_path}")
    else:
        print(f"导出失败: {name}")

    # 清理临时布局
    project.layoutManager().removeLayout(layout)
```

### 5. 目录递归遍历处理

```python
import os
import processing
from qgis.core import QgsVectorLayer, QgsProject

root_dir = "/path/to/data_root"
target_crs = "EPSG:4326"
results = {"success": [], "failed": []}

for dirpath, dirnames, filenames in os.walk(root_dir):
    for filename in filenames:
        if not filename.endswith(('.shp', '.geojson', '.gpkg')):
            continue

        filepath = os.path.join(dirpath, filename)
        name = os.path.splitext(filename)[0]

        try:
            layer = QgsVectorLayer(filepath, name, "ogr")
            if not layer.isValid():
                results["failed"].append((filename, "无效文件"))
                continue

            # 检查是否需要重投影
            if layer.crs().authid() != target_crs:
                result = processing.run("native:reprojectlayer", {
                    'INPUT': layer,
                    'TARGET_CRS': target_crs,
                    'OUTPUT': 'memory:' + name
                })
                QgsProject.instance().addMapLayer(result['OUTPUT'])
                results["success"].append(f"{filename} (重投影)")
            else:
                QgsProject.instance().addMapLayer(layer)
                results["success"].append(filename)

        except Exception as e:
            results["failed"].append((filename, str(e)))

print(f"\n处理完成:")
print(f"  成功: {len(results['success'])} 个文件")
print(f"  失败: {len(results['failed'])} 个文件")
for name, err in results["failed"]:
    print(f"    {name}: {err}")
```

## 脚本编写规范

1. 批量操作必须包含 try-except 错误处理，单个文件失败不中断整体流程。
2. 输出目录使用 `os.makedirs(dir, exist_ok=True)` 确保存在。
3. 处理过程中打印进度信息，便于监控。
4. 批量操作完成后输出汇总报告（成功/失败数量和详情）。
5. 临时资源（布局、图层）在使用后及时清理。

## 常见陷阱

- `os.listdir()` 不递归，需要递归遍历时用 `os.walk()` 或 `glob.glob('**/*.shp', recursive=True)`
- 批量加载大量图层会占用大量内存，处理完一个释放一个
- `processing.run()` 的输出路径不能包含中文（某些算法），建议使用英文路径
- SHP 文件需要 `.shp/.shx/.dbf` 三个文件同时存在，缺少任一会加载失败
- 批量导出地图时，临时布局不清理会导致布局管理器中堆积大量布局
- 内存图层（`'memory:'`）在 QGIS 关闭后丢失，需要持久化时指定文件路径
