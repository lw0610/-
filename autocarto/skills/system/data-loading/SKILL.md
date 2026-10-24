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
---

# 数据加载与导出使用说明

先读工程和图层状态，核实已有图层及真实来源；同源、同子图层的数据已存在时优先复用，data_load_vector/data_load_raster 的 reuse_existing 默认 true。需要独立图层对象时明确传 false，不能按相似名称随意复用。路径按项目根解析，资源清单按基准目录解析，调用后以返回的图层 ID 和真实路径继续。

一组数据用 data_load_list，data_list 传数据清单资源的名称、编号或路径；资源服务负责解析 CSV/XLSX，base_dir 仅覆盖本次调用的基准目录，不修改已保存绑定。数据文件夹先用 resource_get 查看条目，再分别加载其中的矢量或栅格。清单核实名称、路径、类型、必需项、编码和子图层，检查每项 loaded/reused/missing/failed 状态；可选缺失与必需失败区分报告，不把部分成功当完整交付。单矢量/栅格分别用 data_load_vector/data_load_raster；CSV 坐标数据核实坐标列、分隔符与已知 CRS，GPKG 明确子图层。

加载后用 get_layers_info 和详细信息核实数量、几何/波段、字段、CRS、范围及 NoData。没有活动图层不等于没有数据；不猜测不存在的图层。几何检查、栅格统计由相应工具完成，阈值应有任务依据。导出使用独立路径，重新加载或检查产物以验证文件完整性。
