---
name: gps-data
description: GPS 数据下载、导入与轨迹接入工具集合
kind: category
runtime: qgis
enabled: true
tags:
- gps
- data
tools:
- gps_download
- gps_import_gpx
display_name: GPS 数据
domain: 数据
system: false
hidden: false
auto_mount: ''
---

# GPS 数据使用说明

导入前确认文件包含航点、轨迹还是路线，gps_import_gpx 选择对应类型；从设备取数用 gps_download，先核实设备和实际可用格式。CSV 坐标输入须确认坐标列、数值格式、分隔符及已知 CRS，度分秒不能直接当十进制度。不能仅凭文件扩展名断言所有坐标都是 WGS84。

显示和叠加使用 QGIS 动态投影；只有分析、量算明确需要时才转换。先核实源 CRS、坐标单位和算法处理方式，不能因显示 CRS 不同而批量重投影；定义 CRS 只改元数据，重投影才改变坐标。转换时生成独立结果，核对范围、位置和精度。

轨迹连接用 points_to_path 时按真实时间戳或序号排序，按对象分组，检查时间缺失、重复和跳点。需要坐标字段可用 add_xy_fields，说明字段采用的 CRS 和单位。导入后核实真实图层、要素类型、数量与位置；轨迹断裂或顺序异常先查数据，不通过无依据插点掩盖问题。
