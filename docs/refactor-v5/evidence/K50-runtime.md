# K50 运行端收口与回归证据

2026-10-27，本地隔离运行目录，未修改用户配置/会话、未启动真实应用、未连接公网。

## 运行入口

- 控制器仅保留 `StartFromPlan`：删除旧 `Start → Compiler.EnsureRunnable → preparePlan`、旧输入表单转换、条件模型判断、`$acquire/$fill` 执行与预取任务、旧编译器恢复/重新开始路径。
- 数据准备步骤通过统一 `Options.DataGet` 执行；同一获取步骤重试时复用此前已核验成功的数据绑定与来源记录。
- 每步最多自动执行两次；仍失败进入 `needs_replan`。最多三次修订，第四版仍失败则终止。保留正常审查/确认 HITL，删除旧 `failure_choice` 和 skip/ask/stop 失败策略执行分支。
- 历史记录仍按兼容字段读取，但不能执行或隐式重编译；恢复新计划须核对原始消息摘要、资源指纹及当前能力。
- 暂停后重新构造控制器支持修订、跳过明确的可选步骤和取消；取消上下文与恢复执行一致。

## 测试迁移

- 旧表单/编译/条件分支测试随相应产品入口删除。保留真实能力的测试改为新计划入口：产物核验、回执修复、人工确认断点、取消、事件/快照、原文身份、角色隔离、知识执行器及文件索引。
- 新增新计划回归：资源确认期间再次变更拒绝旧确认；磁盘恢复后可选步骤可跳过/必需步骤不可跳过；跨重启三次修订后停止；获取取消不执行后续步骤；第二项失败时第一项不重复下载。
- 范围几何测试直接调用统一服务使用的本地 `ResolveExtent/PrepareExtent`，保留零面积、非有限坐标、投影转换、掩膜与外扩边界回归。来源排序/默认参数测试改为目录规划环境，不再构造旧流程表单。
- Plan 工厂回归通过真正创建并运行智能体验证共享主提示词保留，且 `task/data_get/data_register/osm/browser/web/pyqgis` 执行能力不会从系统专家或工具包重新注入只读 Plan。

## 数据进度与输入边界

- OSM 实际流式下载进度传入统一 acquire 回调，并保留原有 OSM 事件消费者；`flow.acquisition_progress` 包含行、字节、总量、可用百分比、应用读取速率、plan_id/version。总量未知时百分比为空，不把解压后应用读取速度称为线路速度。
- 本机 httptest OSM 服务端走真实 OSM Invoke → acquire → dataservice Get，并验证非零接收字节确实到达服务回调。
- `data_find` 返回的候选文件只读，不登记为新成果；实时及历史恢复索引均覆盖该行为。

## 验证

通过：`go test ./internal/flowrun ./internal/acquire ./internal/agentruntime ./internal/dataservice ./internal/deepagent ./internal/sessionfiles -count=1`。

通过：`go test -race ./internal/flowrun -count=1`，覆盖取消/恢复控制器并发访问。

WebSocket 整体回归由根任务负责；本泳道已同步迁移其候选资源只读测试。真实 QGIS/完整应用端到端结果由 K51/根任务证据记录。
