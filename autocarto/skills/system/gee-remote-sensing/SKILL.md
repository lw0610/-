---
name: gee-remote-sensing
description: Google Earth Engine 遥感检索、云掩膜、影像筛选与合成、质量检查和下载
kind: category
runtime: internal
enabled: true
tools:
- gee_check_environment
- gee_run_script
- gee_task_status
- gee_cancel_task
display_name: 遥感影像（GEE）
domain: 在线数据
system: false
hidden: false
auto_mount: ''
---

# GEE 遥感影像使用说明

先用 gee_check_environment 核实初始化、本地下载和区域读取三个就绪条件。按任务确认可信本地研究区、传感器、时间、波段、分辨率和输出规格，不能把参考中的日期或阈值当统一默认。区域缺失时可用已分配的 admin_boundary_extract 或在线工具获取并核验；不把完整区域偷偷改成估计矩形。

gee_run_script 已提供 ee、output_dir、load_region、check_coverage、heartbeat、download_image 等辅助项，不重新授权或初始化。长操作用阶段心跳，元数据查询限定规模；云掩膜、同日拼接和跨日合成含义不同，记录真实观测日期或合成时段。check_coverage 返回0到1浮点，检查完整ROI与目标波段共同有效覆盖，不把几何覆盖当有效像元覆盖。

用download_image完成本地分块下载，核验范围、波段、CRS、分辨率、掩膜和真实文件，失败块不能忽略。已有同参数且核验通过的产物可复用。只有明确选择云端交付才导出Drive并记录任务编号；COMPLETED不代表本地文件已取回。需要取消时用真实编号。

通过reference_read按需读取：references/GEE执行与质量检查.md、references/image-composite.md、references/classification.md、references/change-detection.md。单日模板说明在templates/single-date-image.md；模板代码由已分配脚本工具读取templates/single_date_image.py，reference_read不开放.py。来源、固定版本和许可见UPSTREAM.md与LICENSE；上游参考及模板原文保留，示例需按当前任务适配。
