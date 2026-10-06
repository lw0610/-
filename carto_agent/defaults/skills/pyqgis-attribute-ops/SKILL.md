---
name: pyqgis-attribute-ops
description: PyQGIS 属性字段操作脚本模板，支持字段增删改、表达式计算、表连接和批量属性处理
kind: workflow
---

# PyQGIS 属性字段操作

## 概述

本 skill 提供 PyQGIS 脚本模板，用于属性字段的增删改查、表达式计算、表连接和批量属性处理。涵盖编辑会话管理、字段计算器和属性表操作。

## 核心 API 参考

| 类/方法 | 用途 |
|---------|------|
| `layer.startEditing()` | 开始编辑会话 |
| `layer.commitChanges()` | 提交编辑 |
| `layer.rollBack()` | 回滚编辑 |
| `layer.dataProvider().addAttributes([field])` | 添加字段 |
| `layer.dataProvider().deleteAttributes([idx])` | 删除字段 |
| `layer.changeAttributeValue(fid, idx, value)` | 修改属性值 |
| `QgsField(name, type)` | 创建字段定义 |
| `QgsExpression(expr_string)` | 创建表达式 |
| `QgsExpressionContext` | 表达式上下文 |

## 代码模板

### 1. 添加字段并赋值

```python
from qgis.core import QgsProject, QgsField
from qgis.PyQt.QtCore import QVariant

layer = QgsProject.instance().mapLayersByName("target_layer")[0]

# 添加字段
layer.startEditing()
layer.dataProvider().addAttributes([
    QgsField("area_km2", QVariant.Double),
    QgsField("category", QVariant.String, len=50),
    QgsField("score", QVariant.Int)
])
layer.updateFields()

# 计算面积并赋值
area_idx = layer.fields().indexOf("area_km2")
for feat in layer.getFeatures():
    area_m2 = feat.geometry().area()
    layer.changeAttributeValue(feat.id(), area_idx, area_m2 / 1e6)

layer.commitChanges()
print(f"已添加字段并计算面积，要素数: {layer.featureCount()}")
```

### 2. 使用表达式批量计算

```python
from qgis.core import (
    QgsProject, QgsExpression, QgsExpressionContext,
    QgsExpressionContextUtils
)

layer = QgsProject.instance().mapLayersByName("target_layer")[0]

expression = QgsExpression('"population" / "area_km2"')
context = QgsExpressionContext()
context.appendScopes(QgsExpressionContextUtils.globalProjectLayerScopes(layer))

layer.startEditing()

# 添加密度字段
from qgis.core import QgsField
from qgis.PyQt.QtCore import QVariant
layer.dataProvider().addAttributes([QgsField("density", QVariant.Double)])
layer.updateFields()

density_idx = layer.fields().indexOf("density")
for feat in layer.getFeatures():
    context.setFeature(feat)
    value = expression.evaluate(context)
    layer.changeAttributeValue(feat.id(), density_idx, value)

layer.commitChanges()
```

### 3. 条件赋值

```python
from qgis.core import QgsProject

layer = QgsProject.instance().mapLayersByName("target_layer")[0]

layer.startEditing()
cat_idx = layer.fields().indexOf("category")

for feat in layer.getFeatures():
    pop = feat["population"]
    if pop > 1000000:
        category = "大城市"
    elif pop > 100000:
        category = "中等城市"
    else:
        category = "小城市"
    layer.changeAttributeValue(feat.id(), cat_idx, category)

layer.commitChanges()
```

### 4. 表连接

```python
import processing
from qgis.core import QgsProject

main_layer = QgsProject.instance().mapLayersByName("main_layer")[0]
join_layer = QgsProject.instance().mapLayersByName("join_table")[0]

result = processing.run("native:joinattributestable", {
    'INPUT': main_layer,
    'FIELD': 'id',
    'INPUT_2': join_layer,
    'FIELD_2': 'main_id',
    'FIELDS_TO_COPY': ['value1', 'value2'],  # 空列表=全部字段
    'METHOD': 0,  # 0=一对一取第一个匹配
    'PREFIX': 'join_',
    'OUTPUT': 'memory:joined_layer'
})

QgsProject.instance().addMapLayer(result['OUTPUT'])
print(f"连接完成，字段数: {result['OUTPUT'].fields().count()}")
```

### 5. 删除字段和重复要素

```python
from qgis.core import QgsProject
import processing

layer = QgsProject.instance().mapLayersByName("target_layer")[0]

# 删除指定字段
fields_to_delete = ["temp_field", "unused_col"]
layer.startEditing()
indices = [layer.fields().indexOf(f) for f in fields_to_delete if layer.fields().indexOf(f) >= 0]
layer.dataProvider().deleteAttributes(indices)
layer.updateFields()
layer.commitChanges()

# 删除重复要素（基于属性）
result = processing.run("native:removeduplicatesbyattribute", {
    'INPUT': layer,
    'FIELDS': ['name', 'type'],
    'OUTPUT': 'memory:no_duplicates'
})
QgsProject.instance().addMapLayer(result['OUTPUT'])
```

### 6. 字段重命名与类型转换

```python
import processing
from qgis.core import QgsProject

layer = QgsProject.instance().mapLayersByName("target_layer")[0]

# 使用 refactor_fields 重命名和转换类型
field_mapping = []
for field in layer.fields():
    mapping = {
        'expression': f'"{field.name()}"',
        'length': field.length(),
        'name': field.name().lower().replace(' ', '_'),  # 重命名：小写+下划线
        'precision': field.precision(),
        'type': field.type()
    }
    field_mapping.append(mapping)

result = processing.run("native:refactorfields", {
    'INPUT': layer,
    'FIELDS_MAPPING': field_mapping,
    'OUTPUT': 'memory:refactored'
})
QgsProject.instance().addMapLayer(result['OUTPUT'])
```

## 脚本编写规范

1. 属性修改必须在 `startEditing()` 和 `commitChanges()` 之间进行。
2. 操作失败时调用 `rollBack()` 回滚，避免数据损坏。
3. 添加字段后必须调用 `updateFields()` 刷新字段缓存。
4. 大量要素修改时，使用 `dataProvider().changeAttributeValues()` 批量提交，性能更好。
5. 表连接前确认连接字段的数据类型一致（字符串 vs 整数）。

## 常见陷阱

- `startEditing()` 后忘记 `commitChanges()` 会导致修改丢失
- `fields().indexOf()` 返回 -1 表示字段不存在，直接使用会导致错误
- `QVariant.String` 默认长度为 0（无限制），某些格式（如 SHP）需要指定 `len` 参数
- SHP 字段名最长 10 个字符，超长会被截断
- `changeAttributeValue` 的 fid 参数必须是 `feat.id()`，不能用自增序号
- 编辑会话中不要同时用 `dataProvider()` 和 `layer` 的方法修改同一数据
