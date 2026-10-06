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
---

# GEE 工具包

提供 Earth Engine 环境检查、脚本执行和云任务管理工具。配套的可读取工作流程为 `gee-scripting`；执行 GEE 任务的智能体应同时分配本工具包与该工作流程。
