---
name: marching-route-data-visualization
description: 行军线路图数据加载。
kind: workflow
---

# 行军路线图底图加载指南

## 概述

该 skill 用于在制作行军路线图时，把 `China_map` 目录下允许清单中的 8 个原始底图图层加载到 QGIS。它只负责原始源复用、缺失源加载和只读检查，不负责 CRS 统一或数据转换。

## 原始底图允许清单

本流程只允许下列图层作为底图：

1. `省域` — `<YOUR_DATA_DIR>/底图数据/China_map/省域.shp`
2. `县级城市驻地` — `<YOUR_DATA_DIR>/底图数据/China_map/县级城市驻地.shp`
3. `铁路` — `<YOUR_DATA_DIR>/底图数据/China_map/铁路.shp`
4. `公路` — `<YOUR_DATA_DIR>/底图数据/China_map/公路.shp`
5. `国道` — `<YOUR_DATA_DIR>/底图数据/China_map/国道.shp`
6. `地区界线` — `<YOUR_DATA_DIR>/底图数据/China_map/地区界线.shp`
7. `河流线` — `<YOUR_DATA_DIR>/底图数据/China_map/河流线.shp`
8. `河流面` — `<YOUR_DATA_DIR>/底图数据/China_map/河流面.shp`

清单中的名称同时作为 `data_load_vector.layer_name`，因此 QGIS 中显示的图层名必须与文件主名一致，不得恢复成旧河南底图名称。

## 加载步骤

1. 先读取当前 QGIS 工程的图层源路径，将 provider 源中的附加参数移除后，按规范化绝对路径与上述允许清单逐项比对。
2. 同一原始源已经存在时，直接复用其 `layer_id`，记录 `load_status=reused`；不再调用 `data_load_vector`。
3. 只对尚未加载的允许源调用一次 `data_load_vector`，使用清单中的固定图层名，记录 `load_status=loaded`。
4. 加载后只读取图层的可用性、范围和 CRS。如果 CRS 不同，在交接中如实列出，然后继续流程。
5. 交付给 cartographer 的底图清单必须是 `artifacts.original_layers`，恰好包含这 8 个原始源。工程内其他图层、旧副本或同名不同源图层均不进入该清单。

## CRS 禁止操作

- 本 skill 下，CRS 差异仅用于诊断报告，不是数据质量错误。显示时使用 QGIS 动态坐标转换。
- 严禁调用重投影、定义/修改 CRS、保存要素、导出图层、格式转换或处理算法来“统一”这 8 个底图。
- 严禁调用 `scripting_specialist` 创建替代图层，严禁创建任何临时、内存、导出或重投影副本。
- 严禁加载名称带 `_3857`、`_proj`、`_src`、`_unified`、`_export` 或 `_lcc` 的副本代替原始源。
- CRS 差异不得写入 `blocking_issues`，不得触发重试，也不得建议 orchestrator 追加 CRS 任务。

## 交接要求

`artifacts.original_layers` 的每项必须包含：

- `name`：允许清单中的固定图层名。
- `source_path`：允许清单中的原始 `.shp` 绝对路径。
- `layer_id`：当前 QGIS 工程中被复用或新加载的原始图层 ID。
- `crs`：读取到的 CRS 诊断值。
- `load_status`：只能是 `loaded` 或 `reused`。

如果某个原始源缺失或无法加载，报告该原始路径并停止；不得使用转换副本替代。

