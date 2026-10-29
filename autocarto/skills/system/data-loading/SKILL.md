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
- layer_load_builtin
- data_load_vector
- data_load_raster
- data_load_list
- data_export_vector
- data_export_raster
- geodata_inspect
- geodata_thumbnail
---

# 数据加载与导出使用说明

加载工具默认将图层显示到面板与画布，按流程规定的**从底到顶**顺序逐个加载即可，不因所处阶段延迟显示。仅为计算打开原始数据或中间结果时明确传 `show=false`，返回的后台 `layer_id` 可直接交给后续工具；分析工具产生的中间结果不会自动上图。本次地图需要显示的业务图层可以直接传已有 `layer_id` 加载，不必另存文件。每次新显示的图层自然放到顶部，已显示的图层再次调用保持原位置，不需要额外排序或事后清理。

系统交接的数据包含 `load_method: layer_load_builtin` 时，按 `builtin_name` 调用 `layer_load_builtin`；默认使用登记的实际坐标系、样式和标注字段，保持原件只读。需要裁剪时传 EPSG:4326 的 extent 和 clip=true，得到会话派生副本。不得把这些已登记图层重新当成缺失数据检索。

只需核验磁盘文件时先用 geodata_inspect，返回子图层、坐标系、范围、数量、字段与少量样本，不会把数据加载到工程。需要预览时使用 geodata_thumbnail，将新 PNG 写入会话临时目录；CSV 坐标表必须明确坐标列和已知坐标系。多图层 GeoPackage 先查看全部子图层，再明确选择。读取成功不代表几何或数据内容完全正确。

先读工程和图层状态，核实已有图层及真实来源；data_load_vector/data_load_raster 对同源、同子图层的数据始终复用，按路径重新调用会刷新文件内容，显式传入名称时更新已有图层名称，不重复加载副本。不同文件和不同子图层仍各自独立，不能按相似名称随意复用。路径按项目根解析，资源清单按基准目录解析，调用后以返回的图层 ID 和真实路径继续。

一组已经确定来源的数据可使用 `data_load_list`，工具按 `items` 原顺序逐项执行，不为凑齐整批数据延迟已可加载的正式图层。每项包含 `name`，以及实际 `path` 或已有 `layer_id`，可选 `kind`（vector/raster）、`required`。仅准备计算输入例如 `{"show":false,"items":[{"name":"分析用站点","path":"会话数据/站点.gpkg","kind":"vector"}]}`；显示已准备图层例如 `{"items":[{"name":"人口背景","layer_id":"实际栅格ID"},{"name":"水域","layer_id":"实际水域ID"}]}`，底层先、顶层后。这些路径或 ID 必须来自用户材料、已核验工程或工具结果，不能猜测；用户给文件夹时先用 `data_find` 的 `provided` 对应真实文件，不使用公共数据清单或固定 CSV 约定。检查每项 loaded/reused/missing/failed 状态，可选缺失与必需失败分别报告，不把部分成功当完整交付。单个矢量/栅格分别用 `data_load_vector`/`data_load_raster`；CSV 坐标数据核实坐标列、分隔符与已知 CRS，GPKG 明确子图层。

显示后的图层可用 get_layers_info 查看；后台对象按实际 layer_id 调用 get_active_layer_info 查询。已有回执明确的数量、字段、CRS和范围直接沿用，只查询尚缺信息；没有活动图层不等于没有数据。几何检查、栅格统计由相应工具完成，阈值应有任务依据。

修正本会话刚下载或生成的错误 GeoJSON 时，保留其明确的来源路径，将修正结果放在内存图层，再调用 data_export_vector：layer 传修正后图层 ID，replace_source 传原文件绝对路径，省略 output。工具先写入并核验，再替换原路径并刷新同源 QGIS 图层；成功后继续使用返回的真实路径，不另存 `_clean`、`_fixed` 或编号副本。替换失败时保留原件并报告原因，不能声称修正已经保存。重新下载以纠正本次 OSM 结果时使用原 output 和 overwrite=true。

只有同一结果的纠错才替换；用户导入的原始文件、另一个任务的结果、明确要求另存或独立分析的派生成果使用独立路径。不能根据相似文件名推断替换关系。本会话创建的内存工作图层（包括筛选结果、字段处理结果）导出后，回执中的 layer_id 继续指向保存后的同一图层，无需重新加载；其他普通导出可用 geodata_inspect 读取产物。
