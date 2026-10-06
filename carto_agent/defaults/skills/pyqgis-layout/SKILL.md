---
name: pyqgis-layout
description: PyQGIS 打印布局脚本模板，支持布局创建、地图要素添加和 PDF/PNG 导出
kind: workflow
---

# PyQGIS 打印布局

## 概述

本 skill 提供 PyQGIS 脚本模板，用于创建打印布局、添加地图要素（地图、图例、比例尺、指北针、标题）和导出 PDF/PNG。

## 核心 API 参考

| 类/方法 | 用途 |
|---------|------|
| `QgsPrintLayout(project)` | 创建打印布局 |
| `QgsLayoutItemMap(layout)` | 地图项 |
| `QgsLayoutItemLegend(layout)` | 图例项 |
| `QgsLayoutItemScaleBar(layout)` | 比例尺项 |
| `QgsLayoutItemLabel(layout)` | 文本标签项 |
| `QgsLayoutItemPicture(layout)` | 图片项（指北针） |
| `QgsLayoutExporter(layout)` | 布局导出器 |
| `QgsLayoutSize(w, h, unit)` | 布局尺寸 |
| `QgsLayoutPoint(x, y, unit)` | 布局位置 |

## 代码模板

### 1. 完整 A4 横向布局创建

```python
from qgis.core import (
    QgsProject, QgsPrintLayout, QgsLayoutItemMap, QgsLayoutItemLegend,
    QgsLayoutItemScaleBar, QgsLayoutItemLabel, QgsLayoutItemPicture,
    QgsLayoutSize, QgsLayoutPoint, QgsUnitTypes, QgsLayoutExporter
)
from qgis.PyQt.QtGui import QFont, QColor

project = QgsProject.instance()
manager = project.layoutManager()

# 创建布局
layout = QgsPrintLayout(project)
layout.initializeDefaults()
layout.setName("my_map_layout")

# 设置页面大小（A4 横向: 297x210mm）
page = layout.pageCollection().page(0)
page.setPageSize(QgsLayoutSize(297, 210, QgsUnitTypes.LayoutMillimeters))

# === 添加主地图 ===
map_item = QgsLayoutItemMap(layout)
map_item.setRect(0, 0, 200, 170)
map_item.attemptMove(QgsLayoutPoint(10, 30, QgsUnitTypes.LayoutMillimeters))
map_item.attemptResize(QgsLayoutSize(200, 170, QgsUnitTypes.LayoutMillimeters))
map_item.setFrameEnabled(True)
map_item.zoomToExtent(project.mapLayersByName("target_layer")[0].extent())
layout.addLayoutItem(map_item)

# === 添加标题 ===
title = QgsLayoutItemLabel(layout)
title.setText("地图标题")
title.setFont(QFont("Microsoft YaHei", 18))
title.attemptMove(QgsLayoutPoint(10, 5, QgsUnitTypes.LayoutMillimeters))
title.attemptResize(QgsLayoutSize(200, 20, QgsUnitTypes.LayoutMillimeters))
title.setHAlign(1)  # 居中
layout.addLayoutItem(title)

# === 添加图例 ===
legend = QgsLayoutItemLegend(layout)
legend.setLinkedMap(map_item)
legend.setTitle("图例")
legend.attemptMove(QgsLayoutPoint(220, 30, QgsUnitTypes.LayoutMillimeters))
legend.attemptResize(QgsLayoutSize(65, 80, QgsUnitTypes.LayoutMillimeters))
legend.setFrameEnabled(True)
layout.addLayoutItem(legend)

# === 添加比例尺 ===
scalebar = QgsLayoutItemScaleBar(layout)
scalebar.setLinkedMap(map_item)
scalebar.setStyle('Single Box')
scalebar.setUnits(QgsUnitTypes.DistanceKilometers)
scalebar.setNumberOfSegments(4)
scalebar.setNumberOfSegmentsLeft(0)
scalebar.setUnitsPerSegment(1)
scalebar.attemptMove(QgsLayoutPoint(10, 205, QgsUnitTypes.LayoutMillimeters))
layout.addLayoutItem(scalebar)

# === 添加指北针 ===
north_arrow = QgsLayoutItemPicture(layout)
north_arrow.setPicturePath(":/images/north_arrows/layout_default_north_arrow.svg")
north_arrow.attemptMove(QgsLayoutPoint(260, 160, QgsUnitTypes.LayoutMillimeters))
north_arrow.attemptResize(QgsLayoutSize(25, 25, QgsUnitTypes.LayoutMillimeters))
layout.addLayoutItem(north_arrow)

# 添加到布局管理器
manager.addLayout(layout)
print("布局创建完成")
```

### 2. 设置地图范围和比例尺

```python
from qgis.core import QgsProject, QgsRectangle

project = QgsProject.instance()
layout = project.layoutManager().layoutByName("my_map_layout")
map_item = [item for item in layout.items() if isinstance(item, QgsLayoutItemMap)][0]

# 方法1：缩放到图层范围
layer = project.mapLayersByName("target_layer")[0]
map_item.zoomToExtent(layer.extent())

# 方法2：设置自定义范围
custom_extent = QgsRectangle(116.0, 39.5, 117.0, 40.5)
map_item.setExtent(custom_extent)

# 方法3：设置固定比例尺
map_item.setScale(50000)  # 1:50000

map_item.refresh()
print(f"地图比例尺: 1:{map_item.scale():.0f}")
```

### 3. 自定义图例

```python
from qgis.core import (
    QgsProject, QgsLayoutItemLegend, QgsLegendStyle
)
from qgis.PyQt.QtGui import QFont

project = QgsProject.instance()
layout = project.layoutManager().layoutByName("my_map_layout")
legend = [item for item in layout.items() if isinstance(item, QgsLayoutItemLegend)][0]

# 设置图例标题
legend.setTitle("土地利用分类")
legend.setAutoUpdateModel(False)  # 关闭自动更新以手动控制

# 设置字体
legend.rstyle(QgsLegendStyle.Title).setFont(QFont("Microsoft YaHei", 12))
legend.rstyle(QgsLegendStyle.Subgroup).setFont(QFont("Microsoft YaHei", 10))
legend.rstyle(QgsLegendStyle.SymbolLabel).setFont(QFont("Microsoft YaHei", 9))

# 移除不需要的图层
model = legend.model()
root = model.rootGroup()
for layer_tree_layer in root.findLayers():
    if layer_tree_layer.name() in ["hillshade", "basemap"]:
        root.removeChildNode(layer_tree_layer)

legend.updateLegend()
```

### 4. PDF 导出

```python
from qgis.core import QgsProject, QgsLayoutExporter
import os

project = QgsProject.instance()
layout = project.layoutManager().layoutByName("my_map_layout")

exporter = QgsLayoutExporter(layout)

# PDF 导出
output_dir = project.homePath() or "/tmp"
pdf_path = os.path.join(output_dir, "output_map.pdf")

pdf_settings = QgsLayoutExporter.PdfExportSettings()
pdf_settings.dpi = 300
pdf_settings.forceVectorOutput = True  # 矢量输出（文字可选中）
pdf_settings.appendGeoreference = True  # 附加地理参考

result = exporter.exportToPdf(pdf_path, pdf_settings)
if result == QgsLayoutExporter.Success:
    print(f"PDF 导出成功: {pdf_path}")
else:
    print(f"PDF 导出失败，错误码: {result}")
```

### 5. PNG 图片导出

仅在用户明确要求图片格式（PNG/JPEG）或缩略预览时使用；如果目标是打印地图或未特别说明，优先只导出 PDF。

```python
from qgis.core import QgsProject, QgsLayoutExporter
import os

project = QgsProject.instance()
layout = project.layoutManager().layoutByName("my_map_layout")

exporter = QgsLayoutExporter(layout)

output_dir = project.homePath() or "/tmp"
png_path = os.path.join(output_dir, "output_map.png")

image_settings = QgsLayoutExporter.ImageExportSettings()
image_settings.dpi = 300
image_settings.cropToContents = False

result = exporter.exportToImage(png_path, image_settings)
if result == QgsLayoutExporter.Success:
    print(f"PNG 导出成功: {png_path}")
else:
    print(f"PNG 导出失败，错误码: {result}")
```

## 脚本编写规范

1. 布局创建后必须调用 `manager.addLayout(layout)` 注册到工程。
2. 地图项必须设置范围（`zoomToExtent` 或 `setExtent`），否则显示空白。
3. 图例必须通过 `setLinkedMap()` 关联地图项。
4. 比例尺的 `setUnitsPerSegment()` 根据地图比例尺合理设置。
5. 导出前检查所有布局项是否正确配置。

## 常见陷阱

- `QgsPrintLayout` 创建后必须调用 `initializeDefaults()` 初始化默认页面
- 布局项的位置和大小单位默认为毫米（`QgsUnitTypes.LayoutMillimeters`）
- `setRect()` 设置的是初始矩形，实际位置和大小由 `attemptMove/attemptResize` 控制
- 指北针的 SVG 路径使用 QGIS 内置资源路径 `:/images/north_arrows/`
- PDF 导出的 `forceVectorOutput=True` 会使文件更大但质量更高
- 布局名称不能重复，创建前检查 `manager.layoutByName()` 是否已存在
- `exportToPdf` 返回枚举值，`Success=0`，需要检查返回值确认导出成功
