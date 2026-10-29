---
name: flow-control
display_name: 制图计划
description: 仅在 Plan 模式下，根据流程与本次需求提出、修订制图计划。
kind: category
runtime: internal
domain: 系统
system: false
hidden: true
auto_mount: main
tools:
  - plan_propose
  - plan_revise
---

本手册只用于 Plan 模式。Build 模式由主智能体按照流程通过 task 委派当前方案的子智能体，依据回执继续推进；不提交本手册的计划，不在提交计划后等待控制器接管。

Plan 模式用 plan_propose 声明本次目标、数据和业务阶段，只生成可供确认的计划，不执行工具操作或下载。流程全文已注入时不重复读取；未注入时用 skill 读取 guide-<id>。系统补编号版本、对齐引用，计划执行阶段再准备数据。

每步role_hint从当前方案roles目录选子智能体ID，按职责委派，沿用其模型、提示词和工具。不要生成临时角色或重新拼工具包；具体工具参数由子智能体决定。

按schema写goal、guide、data、steps、deliverables、checks。source=user优先用材料清单ID，也接受路径、附件:名称、会话文件ID；“消息原文”只指用户粘贴正文，不能代替附件。project用图层ID/名称；builtin用内置名称；retrieve用need声明类别/参数/字段。用户/工程数据优先，再点名内置、匹配库与检索；method仅用户指定时填。

范围对应实际数据。先筛选目标区域并声明产出，后续下载extent.from引用该产出，不能引用全国原数据；筛选步骤inputs不含依赖其范围的下载数据。已知地区可用extent.region；expand:0.05表示外扩5%。

inputs引用data.id或前序produces.id，产出不重复放进data。路线抽取产出format:route_dataset。仅用户明确指定使用样式表或符号表时，才在parameters传route_style或symbols的预设名称；否则省略，使用原版内置规则。配色要求不等于选用样式表。已有确认数据集在data绑定真实来源ref，直接供后续inputs使用，不重复抽取。

按流程阶段组织步骤，同一角色连续操作可放在同一步，不按工具或版面元素拆分。instructions写目标、输入和用户要求；上下文已有流程，不要求图层参数表或重抄全文。未要求自动补充时，不把提及的图层种类一律声明为retrieve。

plan_revise提交完整计划，保留已完成结果。预检缺口按建议修订；其余接受后结束本轮，由执行器推进。Plan模式只预览。
