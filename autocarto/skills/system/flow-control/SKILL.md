---
name: flow-control
display_name: 制图计划
description: 根据指南与本次情况提出、修订可执行且可核验的制图计划。
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

# 制图计划

先理解用户目标，按指南索引读取适用指南。指南只描述领域知识，不规定角色与固定步骤。用户指定指南时直接使用该指南。优先从原文、附件、工程图层、已有文件、数据记忆和默认值确定信息；只询问确实缺少且影响结果的项，一次问完。

多阶段任务的进度由主智能体负责。在开始查询数据或委派执行前，用 `write_todos` 列出完成本次目标所需的主要步骤，供右侧“任务与过程”显示；没有匹配指南的复杂任务也适用。步骤按本次需求和依赖组织，不照抄指南目录或预设固定步骤。单次查询、加载文件、修改样式等简单任务无需清单。

执行期间更新同一份清单：未开始为 `pending`，当前阶段为 `in_progress`，实际完成后才为 `completed`。需要人工核对时，将核对与后续制图分成不同步骤；委派前把当前阶段标为进行中，等待确认、工具失败或子任务尚未返回时保留未完成状态。恢复后根据真实结果继续更新，不重新开始整份任务。`write_todos` 只展示进度，不代替 `plan_propose`；已有正式计划时使用计划自身的步骤与状态，不另建重复清单。

用户指定流程全文已注入时直接使用；否则用 `skill({"skill":"guide-<id>"})` 读取，不猜文档路径。`data_find` 只查询数据来源，不能查询流程文档。`need.what` 是对象，类别编号、参数名与允许值使用当前数据类别目录；已知类别用 category，具体指标、年份和用途可同时写 description。extent 使用单一地区名称、真实文件/图层或 bbox，行政层级放 params，指标和分组说明不要拼入地区名称。查询时设置 `include_fallback=true`，以便一次判断已有、可直接获取、需探索或需补充信息。`needs_input` 先补缺项，`unavailable` 明确报告阻塞，不能冒充可执行来源。

匹配指南、需要获取数据或预计三步以上时出计划。数据来源先通过 data_find 查询，复用真实 find_id 与来源选择，不编造。已有数据写 user/project/session；待获取数据写 from=acquire。确定性候选由执行器直接获取；只有 fallback 时也保留同次查询与实际兜底途径，由执行器转入探索取数，取得并登记核验后才交给后续步骤。fallback 代表待探索途径，不代表已有文件；不要为了提交计划先自由委派取齐数据，也不要把同一查询反复换名重查。查询明确要求补参数或澄清需求时，先解决该缺项。原文数据使用 from=conversation，路线知识专家读取系统保留的原文。

新计划的 steps 只列尚需执行的工作；修订正在执行的同一计划时，保留其已完成步骤和产出记录。本会话已经生成、核对或确认的成果使用 `from=session`，按工具返回的成果引用交给后续步骤；`from=step` 引用同一计划内的生产步骤。引用校验失败时修正成果引用，不为绕过错误新增重复抽取或生成步骤；在 instructions 中写“已完成”不会让系统跳过该步骤。只有用户明确要求重新处理时才安排重做。

每步声明所需工具包 ID（capabilities）与可核验的产出，角色由系统按能力选择。capabilities 不是具体工具函数名，不猜测能力；只从当前注入的能力目录或校验诊断的 allowed_capabilities 选择。具体工具调用由执行智能体决定。

系统执行器的输入与参数要求见目录中的 `executors`。例如需要 `conversation_text` 时，在 data 中声明一项 `from=conversation` 原文，并在对应步骤的 uses 中引用它；facts 中对原文的描述不等于输入绑定。`required_parameters` 中的参数写入步骤的 `parameters` 对象；路线抽取的 `route_style`、`symbols` 使用本次指南或用户选择的预设名称，不能只写在 instructions 中。

计划图层声明 `layer_name`、`geometry`，需要样式时使用资源库真实名称；`layer` 仅表示已有工程图层。来源为 `acquire` 时复用本次 `find_id` 和 `choice`（按行选择真实候选 ID，或 `{"*":"recommended"}`）。需要上游数据确定范围时在计划声明依赖，执行器把已经核验的上游结果传给获取专家。不要重新查询已冻结的需求，也不要把有待探索的来源写成已有文件。

`uses` 表示消费的数据，使用 `data[].id`；`depends_on` 使用前序步骤 ID。消费步骤产出时声明 `data:{id:"derived",name:"中文显示名",from:"step",step:"producer",output:"result"}`，其中 `output` 选择生产步骤的 `outputs[].label`，`name` 仅作显示，不必与产出标识相同。显式 `output` 必须匹配；省略时系统兼容旧 `name` 匹配，或根据唯一产出补齐。`uses` 若填写前序 acquire 步骤 ID，系统会展开其数据 ID；前序普通步骤只有一个产出时也可确定性绑定。多产出必须明确选择，诊断会返回 `valid_data_ids` 与 `valid_outputs`，不要猜测。数据 ID 与步骤 ID 同名时优先数据 ID。

只引用更早步骤的产出，不能形成前向或循环依赖。步骤 `outputs[].label` 必须非空且在本步骤内唯一；最终 `deliverables` 的 `label`、`type`、`format` 对应同一步骤产出。只引用本次实际选择的样式；流程列出的不同层级或可选样式不要求全部使用。计划中的事实写清来源；推断和默认值不能假装来自用户。高影响假设用 `impact=high`。

plan_propose 校验失败时读取结构化 diagnostics、allowed_capabilities 和 next_action，修正指出的字段；不要原样重试，不重复展开同一套业务推理。来源已有真实探索途径时，可直接规划；联网授权由执行阶段的数据服务按设置处理，不在规划前重复询问。已有明确默认值的分级方法、配色等写入 assumptions，不逐项询问；仅未说明且会改变结果的范围或数据口径才需要澄清。成功后结束本轮，由系统根据设置决定是否等待确认并逐步执行，包括必要的数据探索获取。不要在提交后自行委派执行同一计划。用户修改要求或步骤失败时用 plan_revise 提交完整新版本，只修订尚未完成部分，保留已完成结果。简单的加载文件、改样式、查信息可以直接委派，不强制计划。

Plan模式只读和出计划，禁止获取数据或执行；切换Build后执行同一计划，系统再次校验。
