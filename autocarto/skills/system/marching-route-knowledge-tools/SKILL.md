---
name: marching-route-knowledge-tools
description: 行军路线任务知识图谱正式抽取契约、校验和六表编译工具集合
kind: category
runtime: qgis
enabled: true
tags:
- knowledge-extraction
- marching-route
- cartography
tools:
- marching_route_get_extraction_contract
- marching_route_compile_dataset
display_name: 路线知识
domain: 系统
system: false
hidden: true
auto_mount: executor:route_knowledge
---

# 行军路线知识编译工具

`marching_route_get_extraction_contract` 返回原文绑定的 `contract_id` 和 `json_schema`，专用运行时以 `compact=true` 获取。业务规则来自知识智能体的配置，模型调用由运行时完成。

`marching_route_compile_dataset` 接收结构化对象 `knowledge_json`、原 `contract_id` 和未改写的 `source_text`，负责校验与六表编译，不调用模型。正常任务使用当前会话产物，完整保留工具返回的数据集路径、行数、验证结果和警告，供下游通过 `dataset_source=current_session` 读取。
