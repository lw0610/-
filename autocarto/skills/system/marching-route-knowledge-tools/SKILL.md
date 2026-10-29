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

路线抽取默认使用原版内置的路线绘制规则、事件符号与 SVG，生成六表及 symbol_mapping.csv，不加载路线样式表或外部符号表。仅用户明确指定使用表中内容时，才传入可选的 `parameters.route_style` 或 `parameters.symbols` 预设名称；配色要求不等于选择样式表。真实 `source_ref/source_refs` 始终传递原文，不替换为任务要求。确认仅表示接受已保存事实，后续生成数据使用该确认稿。

已有用户提供或本次已确认生成的路线数据集时，规划只绑定其真实来源，后续步骤读取数据集；没有明确要求重新抽取就不再次调用抽取能力，不把同一数据集同时声明为输入和新产出。只修正绘制预设时复用已确认知识，不因修改颜色或符号重读史料。
