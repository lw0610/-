---
name: pyqgis-styling
description: PyQGIS 符号化渲染脚本模板，支持分级/分类/规则渲染、标注配置和颜色方案设置
kind: workflow
---

# PyQGIS 符号化渲染

## 概述

本 skill 提供 PyQGIS 脚本模板，用于矢量图层的符号化渲染和标注配置。涵盖分级渲染、分类渲染、规则渲染、热力图样式和标注设置。

## 核心 API 参考

| 类/方法 | 用途 |
|---------|------|
| `QgsGraduatedSymbolRenderer` | 分级渲染器 |
| `QgsCategorizedSymbolRenderer` | 分类渲染器 |
| `QgsRuleBasedRenderer` | 规则渲染器 |
| `QgsSingleSymbolRenderer` | 单一符号渲染器 |
| `QgsSymbol.defaultSymbol(geom_type)` | 默认符号 |
| `QgsGradientColorRamp` | 渐变色带 |
| `QgsStyle.defaultStyle()` | 默认样式库 |
| `QgsPalLayerSettings` | 标注设置 |
| `QgsVectorLayerSimpleLabeling` | 简单标注 |
| `QgsTextFormat` | 文本格式 |

## 代码模板

### 1. 分级渲染（连续数值字段）

```python
from qgis.core import (
    QgsProject, QgsGraduatedSymbolRenderer, QgsSymbol,
    QgsGradientColorRamp, QgsRendererRange, QgsClassificationJenks
)
from qgis.PyQt.QtGui import QColor

layer = QgsProject.instance().mapLayersByName("districts")[0]

# 创建颜色渐变
color_ramp = QgsGradientColorRamp(
    QColor(255, 255, 178),  # 浅黄
    QColor(189, 0, 38)      # 深红
)

# 创建分级渲染器
renderer = QgsGraduatedSymbolRenderer()
renderer.setClassAttribute("population")
renderer.setSourceColorRamp(color_ramp)

# 使用自然断点分级
classification = QgsClassificationJenks()
renderer.setClassificationMethod(classification)
renderer.updateClasses(layer, 5)  # 5 个分级

layer.setRenderer(renderer)
layer.triggerRepaint()
print(f"分级渲染完成: {renderer.ranges().__len__()} 个等级")
```

### 2. 分类渲染（离散分类字段）

```python
from qgis.core import (
    QgsProject, QgsCategorizedSymbolRenderer, QgsRendererCategory,
    QgsSymbol, QgsFillSymbol
)
from qgis.PyQt.QtGui import QColor

layer = QgsProject.instance().mapLayersByName("landuse")[0]

# 定义分类颜色映射
categories_config = {
    '住宅': QColor(255, 255, 0),
    '商业': QColor(255, 0, 0),
    '工业': QColor(128, 0, 128),
    '绿地': QColor(0, 128, 0),
    '水域': QColor(0, 0, 255),
    '农田': QColor(139, 69, 19)
}

categories = []
for value, color in categories_config.items():
    symbol = QgsFillSymbol.createSimple({
        'color': color.name(),
        'outline_color': '#333333',
        'outline_width': '0.3'
    })
    cat = QgsRendererCategory(value, symbol, value)
    categories.append(cat)

renderer = QgsCategorizedSymbolRenderer("type", categories)
layer.setRenderer(renderer)
layer.triggerRepaint()
print(f"分类渲染完成: {len(categories)} 个类别")
```

### 3. 规则渲染

```python
from qgis.core import (
    QgsProject, QgsRuleBasedRenderer, QgsSymbol, QgsFillSymbol
)
from qgis.PyQt.QtGui import QColor

layer = QgsProject.instance().mapLayersByName("buildings")[0]

# 创建根规则
root_rule = QgsRuleBasedRenderer.Rule(None)

# 规则1：高层建筑（红色）
symbol1 = QgsFillSymbol.createSimple({'color': '#ff0000', 'outline_color': '#333333'})
rule1 = QgsRuleBasedRenderer.Rule(symbol1)
rule1.setLabel("高层建筑 (>50m)")
rule1.setFilterExpression('"height" > 50')
root_rule.appendChild(rule1)

# 规则2：中层建筑（橙色）
symbol2 = QgsFillSymbol.createSimple({'color': '#ff8800', 'outline_color': '#333333'})
rule2 = QgsRuleBasedRenderer.Rule(symbol2)
rule2.setLabel("中层建筑 (20-50m)")
rule2.setFilterExpression('"height" > 20 AND "height" <= 50')
root_rule.appendChild(rule2)

# 规则3：低层建筑（黄色）
symbol3 = QgsFillSymbol.createSimple({'color': '#ffff00', 'outline_color': '#333333'})
rule3 = QgsRuleBasedRenderer.Rule(symbol3)
rule3.setLabel("低层建筑 (≤20m)")
rule3.setFilterExpression('"height" <= 20')
root_rule.appendChild(rule3)

renderer = QgsRuleBasedRenderer(root_rule)
layer.setRenderer(renderer)
layer.triggerRepaint()
```

### 4. 热力图样式（点图层）

```python
from qgis.core import (
    QgsProject, QgsSymbol, QgsGradientColorRamp
)
from qgis.PyQt.QtGui import QColor

layer = QgsProject.instance().mapLayersByName("events")[0]

# 使用 Processing 生成热力图栅格
import processing
result = processing.run("native:heatmapkerneldensityestimation", {
    'INPUT': layer,
    'RADIUS': 500,           # 搜索半径
    'PIXEL_SIZE': 50,        # 像元大小
    'WEIGHT_FIELD': 'count', # 权重字段（可选）
    'KERNEL': 0,             # 0=四次, 1=三角, 2=均匀, 3=Epanechnikov
    'OUTPUT': 'TEMPORARY_OUTPUT'
})

from qgis.core import QgsRasterLayer
heatmap = QgsRasterLayer(result['OUTPUT'], "heatmap")
QgsProject.instance().addMapLayer(heatmap)

# 设置伪彩色渲染
from qgis.core import QgsSingleBandPseudoColorRenderer, QgsRasterShader, QgsColorRampShader
shader = QgsRasterShader()
color_ramp_shader = QgsColorRampShader()
color_ramp_shader.setColorRampType(QgsColorRampShader.Interpolated)
color_ramp_shader.setColorRampItemList([
    QgsColorRampShader.ColorRampItem(0, QColor(255, 255, 255, 0), "低"),
    QgsColorRampShader.ColorRampItem(0.5, QColor(255, 255, 0), "中"),
    QgsColorRampShader.ColorRampItem(1, QColor(255, 0, 0), "高")
])
shader.setRasterShaderFunction(color_ramp_shader)
renderer = QgsSingleBandPseudoColorRenderer(heatmap.dataProvider(), 1, shader)
heatmap.setRenderer(renderer)
heatmap.triggerRepaint()
```

### 5. 标注配置

```python
from qgis.core import (
    QgsProject, QgsPalLayerSettings, QgsVectorLayerSimpleLabeling,
    QgsTextFormat, QgsTextBufferSettings
)
from qgis.PyQt.QtGui import QColor, QFont

layer = QgsProject.instance().mapLayersByName("cities")[0]

# 文本格式
text_format = QgsTextFormat()
text_format.setFont(QFont("Microsoft YaHei", 10))
text_format.setSize(10)
text_format.setColor(QColor(0, 0, 0))

# 文本描边（光晕）
buffer_settings = QgsTextBufferSettings()
buffer_settings.setEnabled(True)
buffer_settings.setSize(1.5)
buffer_settings.setColor(QColor(255, 255, 255))
text_format.setBuffer(buffer_settings)

# 标注设置
label_settings = QgsPalLayerSettings()
label_settings.fieldName = "name"
label_settings.setFormat(text_format)
label_settings.placement = QgsPalLayerSettings.OverPoint  # 点上方
label_settings.priority = 5  # 优先级 0-10

# 避让设置
label_settings.displayAll = False  # 允许避让隐藏

# 应用标注
labeling = QgsVectorLayerSimpleLabeling(label_settings)
layer.setLabeling(labeling)
layer.setLabelsEnabled(True)
layer.triggerRepaint()
print("标注配置完成")
```

## 脚本编写规范

1. 设置渲染器后必须调用 `layer.triggerRepaint()` 刷新显示。
2. 分级渲染优先使用自然断点（Jenks），数据量大时用等间距。
3. 颜色方案遵循制图规范：连续数据用渐变色，分类数据用对比色。
4. 标注字体优先使用系统中文字体（如 Microsoft YaHei、SimHei）。
5. 规则渲染的表达式语法与 QGIS 表达式一致，字段名用双引号。

## 常见陷阱

- `QgsGraduatedSymbolRenderer` 的 `updateClasses()` 需要先设置 `classAttribute` 和 `classificationMethod`
- 分类渲染的 value 类型必须与字段类型匹配（字符串 vs 数字）
- `QgsFillSymbol.createSimple()` 的颜色参数是字符串格式的十六进制或 `'r,g,b,a'`
- 标注的 `fieldName` 可以是表达式，如 `"name" || '\n' || "population"`
- 热力图的 `RADIUS` 单位取决于图层 CRS，投影坐标系为米
- 渲染器设置不会自动保存到工程，需要 `QgsProject.instance().write()` 保存
