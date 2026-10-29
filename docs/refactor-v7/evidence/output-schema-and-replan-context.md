# 产出 schema 对齐与重规划输入核查

日期：2026-10-29。复现来自隔离真实模型/QGIS 回放 `route-restored-checkpoint-v7-20261029-190819`，会话 `07b6c575-15a1-4d7c-902d-4cf13aa2fcb1`。本次没有修改该会话或运行记录，没有发起新的模型调用。

## 已修正的公开契约

原计划为零项图文标注声明了必需图层 `callouts_done`。执行器实际确认 `callouts.csv` 仅含表头，三次回执分别没有该产出或给出空数组，图层核验正确地拒绝不存在的成果。此前公开 schema 仅能表示实体成果和审查，没有公开执行器已有的文字报告与确认结果，导致模型缺少适当的声明方式。

`plan_propose.yaml` 与 `plan_revise.yaml` 现在完整公开九种产出：

- `layer`、`table`、`dataset`、`file`、`layout`。
- `review_initial`、`review_final`。
- `text`、`confirmation`。

两处均设置 `steps.items.required: [produces]`、`produces.minItems: 1`，对齐 `plan.ValidateV2` 已有的“步骤需要声明可核验的 produces 产出”。说明明确：零项任务可以省略无操作步骤，或以 `text` 如实报告；不能声明不存在的图层或文件。步骤产出不必在 `data` 中重复声明。数据输入类型仍为 `layer/table/dataset/file`，没有把文字、版面或审查结果变成取数类型。

验证：

```text
go test ./internal/skill -run TestL30RealToolSchemaReachesModelWithNestedContract -count=1
ok github.com/autocarto/carto_agent/internal/skill 0.739s
```

该测试加载两份真实 YAML，经工具适配器转换为模型实际接收的 JSON schema，再核对完整枚举、步骤必需产出、最少一项及数据类型边界。没有放宽实体图层核验，也没有把空图层输出当作成功。

## 重规划只读发现

核查时 `websocket/plan_runtime.go:948–950` 将整个 `RunState` 序列化并拼接完整 `Session.GetMessages()`；之后 `injectCartographicRules` 仍按用户 `guide_id` 加载指南。以下是本次运行的事实，而非对模型内部推理的猜测：

- 持久化运行状态的紧凑 JSON 为 103,370 字节。它同时包含当前 CartoPlan、历史计划、内部可执行 FlowPlan、指南正文及每一步所有尝试。
- `load_basemap` 已完成但仍有七次历史尝试，里面包含过期 layer ID；本次失败的 `callouts` 有三次模型自称 completed、系统核验失败的回执。
- 会话最后保存 16 条消息（1 条用户、15 条 assistant）；最后一条是本次重规划答复，因此重规划输入历史为此前 15 条。历史中包含多轮旧的加载总结，以及三条“零项 completed”的步骤回复。
- 回放 `events.jsonl` 第 329–333 行显示主智能体只输出文字，声称计划已 ready、执行器会继续；没有调用 `plan_revise`。
- `replanCartographic` 当时只返回 `result.Err`。事件消费器 `task_runner.go:844–845` 将没有待启动计划的普通结束标成 completed；上层 `runCartographicPlan:673–675` 未取得新计划时返回 nil。因此聊天结束和计划完成存在混淆，原 FlowRun 仍为 needs_replan。

历史冗余与冲突有实际证据；但仅凭日志无法证明它是模型未调用修订工具的唯一原因。

## 建议的最小输入职责（本次未修改实现）

重规划应由代码从已验证运行状态投影一次，而不是额外调用模型压缩：

1. 当前有效 CartoPlan 一份，包含计划 ID/版本、目标、事实、数据需求、尚未完成步骤与交付要求。
2. 当前失败步骤 ID、最近真实回执、核验错误和用户补充意见；重复的历史失败只保留有区别的原因。
3. 已完成步骤 ID、实际产出的类型与值，保留图层 ID、数据路径、版面名称等真实引用。
4. 已准备好的 Inputs/AcquiredInputs、需要定位或解释来源的 InputDetails，以及实际 DataPreparation 取数台账。仍保留尝试过的方法、失败原因、原始文件路径与当前可复用结果。
5. 当前预算、修订计数、有效用户要求和附件引用；指南按当前 guide_id 注入一次。

无需携带内部 FlowPlan 与角色提示词、整个 PlanHistory、已完成步骤的全部 attempts/before_layers，或旧 assistant 自述。既有指南与附件上下文注入依赖用户消息元数据，精简时需保留当前有效引用，不能只剩一段无元数据的文字。

重规划结束还应以实际接受的修订或实际 HITL 等待为依据；普通文字答复不能替代这两个动作。以上为调查建议，重规划生产实现由主任务处理。

## 重规划输入与后置条件修正

同日完成生产接线：`websocket/plan_runtime.go` 在模型调用前直接投影已验证状态，不调用额外模型做摘要。

- 当前 CartoPlan 保留一份，已完成步骤移到带 Product 类型和真实值的 `completed_steps`；原持久计划不修改。
- 当前失败只携带最新回执、核验错误、最后失败、用户指导及该轮预算。旧 attempts、before_layers、PlanHistory、内部 FlowPlan、重复指南正文不进入输入。
- 保留当前 Inputs/AcquiredInputs/InputDetails、DataPreparation 实际台账、数据选择、来源说明与修订剩余次数。
- 仅保留真实 user 消息与附件/指南元数据，不携带 assistant 历史；原始请求有保留，指南仍经现有注入路径加载一次。
- 仍只使用 Plan 智能体。重规划目标 ID/版本随既有阶段参数传递，不回退 Build，不新增模型重试。
- 初次返回和真实 ADK HITL 恢复均核对同计划更高版本的待执行修订或已登记真实中断。口头 ready、无工具答复、残留旧版本 pending 都返回“未提交修订，已保留成果”。阶段普通文本不发布为最终答复，不保存为会话事实，也不触发 completed。
- 既有 `plan.confirm=always` 产生的同计划更高版本、`awaiting_confirmation` 且 `NeedsConfirmation=true` 的已存计划，同样是有效提交；保持真实等待，不自动排队或批准，也不误报“未提交”。该分支通过真实 plan service 与配置验证。
- 同计划更高版本 `needs_data` 也是 RevisePlan 已保存并返回 `accepted:true` 的修订。保留计划卡中不可用数据及补数据提示、会话暂停等待，不自动执行、不重试模型、不误报“未提交”。回归使用实际缺失本地文件，经真实 RevisePlan/预检生成该状态，并验证只有一次 planner 调用。

验证使用隔离 mock planner 经过真实 runner/事件消费、真实 plan service 持久修订和真实 ADK interrupt/resume；没有新真实模型调用。覆盖去除旧状态而保留真实引用、指南只注入一次、无动作拒绝、旧 pending 拒绝、实际修订接受、实际 HITL 等待、HITL 后只文字拒绝/实际修订接受，以及 Plan 不可用时不回退 Build。

`go test ./internal/websocket -count=1` 通过（33.406 秒）；对应定向 race 通过（9.883 秒）。补入原确认策略分支后，再次定向 race 通过（9.600 秒）。真实模型额度此时返回 402，本记录不把确定性测试当作真实路线重新验收成功。
