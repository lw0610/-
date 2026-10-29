---
name: data-loading
display_name: 数据加载与导出
description: QGIS 工程中的图层加载、导出与数据接入工具集合
kind: category
domain: 数据
runtime: qgis
enabled: true
system: false
hidden: false
auto_mount: ''
tags:
- data
- loading
tools:
- data_load_vector
- data_load_raster
- data_load_list
- data_export_vector
- data_export_raster
- geodata_inspect
- geodata_thumbnail
---

# 数据加载与导出使用说明

只需核验磁盘文件时先用 geodata_inspect，返回子图层、坐标系、范围、数量、字段与少量样本，不会把数据加载到工程。需要预览时使用 geodata_thumbnail，将新 PNG 写入会话临时目录；CSV 坐标表必须明确坐标列和已知坐标系。多图层 GeoPackage 先查看全部子图层，再明确选择。读取成功不代表几何或数据内容完全正确。

先读工程和图层状态，核实已有图层及真实来源；同源、同子图层的数据已存在时优先复用，data_load_vector/data_load_raster 的 reuse_existing 默认 true。需要独立图层对象时明确传 false，不能按相似名称随意复用。路径按项目根解析，资源清单按基准目录解析，调用后以返回的图层 ID 和真实路径继续。

一组已经确定来源的数据使用 `data_load_list`，传 `items` 内联列表，每项包含 `name`、实际 `path`，可选 `kind`（vector/raster）、`required`、`layer_id`（复用已有工程图层）；例如 `{"items":[{"name":"河流","path":"会话数据/河流.gpkg|layername=river","kind":"vector","required":true}],"reuse_existing":true}`。这些路径必须来自用户材料、已核验工程或数据服务结果，不能猜测；用户给文件夹时先用 `data_find` 的 `provided` 对应真实文件，不使用公共数据清单或固定 CSV 约定。检查每项 loaded/reused/missing/failed 状态，可选缺失与必需失败分别报告，不把部分成功当完整交付。单个矢量/栅格分别用 `data_load_vector`/`data_load_raster`；CSV 坐标数据核实坐标列、分隔符与已知 CRS，GPKG 明确子图层。

加载后用 get_layers_info 和详细信息核实数量、几何/波段、字段、CRS、范围及 NoData。没有活动图层不等于没有数据；不猜测不存在的图层。几何检查、栅格统计由相应工具完成，阈值应有任务依据。

修正本会话刚下载或生成的错误 GeoJSON 时，保留其明确的来源路径，将修正结果放在内存图层，再调用 data_export_vector：layer 传修正后图层 ID，replace_source 传原文件绝对路径，省略 output。工具先写入并核验，再替换原路径并刷新同源 QGIS 图层；成功后继续使用返回的真实路径，不另存 `_clean`、`_fixed` 或编号副本。替换失败时保留原件并报告原因，不能声称修正已经保存。重新下载以纠正本次 OSM 结果时使用原 output 和 overwrite=true。

只有同一结果的纠错才替换；用户导入的原始文件、另一个任务的结果、明确要求另存或独立分析的派生成果使用独立路径。不能根据相似文件名推断替换关系。普通导出后重新加载或检查产物，以验证文件完整性。
