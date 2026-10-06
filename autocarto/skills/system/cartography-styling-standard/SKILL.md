---
name: cartography-styling-standard
description: 制图符号化协作指南。用于用户对 QGIS 工程图层进行地图符号化。
---
# 制图符号化指南

## 概述

该 skill 用于把 QGIS 工程中的图层转换为更清晰、统一的制图表达。先识别图层承载的地理语义，再选择合适的符号化工具和参数。

## 判断优先级

优先按以下顺序理解图层：

1. 用户明确指定的图层类型或制图意图。
2. 图层名表达的语义，例如“地级城市驻地”“县城驻地”“铁路”“高速公路”“国道”。
3. 图层字段，例如 `name`、`type`、`level`、`class`、`admin_level`。
4. 当前渲染器、已有标注和图例表现。

如果图层名已经表达了类别，通常不要再用字段内容做分类渲染。

## 点图层

使用 `layer_set_marker_symbol` 处理标准居民地驻地点。

参数：

- `layer`：图层名。
- `layer_type`：`city` 或 `county`。
- `label_field`：标注字段，常见为 `name`。

选择建议：

- 地级城市、城市驻地、市级行政中心：`city`。
- 县城、县级驻地、县级行政中心：`county`。

示例：

```yaml
layer: 河南省地级城市驻地
layer_type: city
label_field: name
```

```yaml
layer: 河南省县城驻地
layer_type: county
label_field: name
```

## 线图层

使用 `layer_set_line_symbol` 处理标准交通线。

参数：

- `layer`：图层名。
- `line_type`：`铁路`、`高速公路` 或 `国道`。

选择建议：

- 图层名或字段表达铁路时，传 `铁路`。
- 图层名或字段表达高速、快速路、高速公路时，传 `高速公路`。
- 图层名或字段表达国道、G 字头道路时，传 `国道`。

示例：

```yaml
layer: 河南铁路
line_type: 铁路
```

```yaml
layer: 河南高速公路
line_type: 高速公路
```

```yaml
layer: 河南国道
line_type: 国道
```

## 输出检查清单

- 已确认目标图层的几何类型、图层名语义和必要字段。
- 点图层已按 `city` / `county` 选择 `layer_type`，并设置了正确的 `label_field`。
- 线图层已按 `铁路` / `高速公路` / `国道` 选择 `line_type`。
- 未调用本 skill 未列出的额外符号化工具。
- 面图层未被主动重设样式，除非用户明确要求。
- 不确定图层语义时，已基于工程信息、字段值样本或图层名做判断；仍不确定时已向用户确认。

常见推断结果应落到明确参数上，例如：

- `河南省地级城市驻地` → `layer_type: city`
- `河南省县城驻地` → `layer_type: county`
- `河南铁路` → `line_type: 铁路`
- `河南高速公路` → `line_type: 高速公路`
- `河南国道` → `line_type: 国道`