---
name: admin-boundaries
display_name: 中国行政区边界
description: 按级别和精确名称或行政区代码提取离线边界，校验来源与面几何，报告重名候选。
kind: category
domain: 数据
runtime: qgis
enabled: true
system: false
hidden: false
auto_mount: ''
tools:
- admin_boundary_extract
---

# 中国行政区边界

使用 `admin_boundary_extract` 准备真实行政区范围。`level` 选择省、市或县，提供精确全名或六位行政区代码；已知代码时优先使用代码。名称与代码同时填写时必须同时匹配。

名称重名时返回候选身份，不生成或加载任何边界。根据候选的级别与代码明确目标后重新调用，不把包含关键词或同名的第一项当成目标。

工具读取自带的离线边界数据。每次检查文件大小、来源哈希、身份字段和所选原始面几何；保留原始 WGS84 经纬度坐标和属性，不做简化、修复、缓冲或基准替换。

成功返回真实图层标识、所选行政区、边界文件、来源记录、几何检查结果及资源来源。输出文件可使用项目相对路径；省略时保存到项目运行数据目录。重复调用复用同一份校验结果和图层，不覆盖内容不同的现有文件。

边界数据的来源与许可由原数据包中的 `LICENSE`、`UPSTREAM.md`、`upstream-manifest.json` 和 `upstream/` 保留。来源记录说明数据出处，不能替代法定勘界或测绘认证。
