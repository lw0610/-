# 按用户要求移除执行预算

2026-10-29。用户明确要求删除新增的执行预算；此前 `EXECUTION_BUDGET_REACHED` 会在累计 60 次工具调用时暂停步骤。本次直接删除相关实现，不改为超大值、不保留停用开关。

## 删除范围

- `config/acquisition.go`：删除取数时间/次数、步骤时间/次数、整次运行时间五个配置字段、默认值和校验。
- 删除 `internal/executionbudget` 包，以及工具包装器的调用计数和该包专用终止错误分支。
- `flowrun/controller.go`、`prepare_v2.go`：删除全程/步骤/取数执行 deadline、累计计数、预算进度事件和预算耗尽暂停分支。
- `flowrun/types.go`、`ledger.go`：删除预算状态字段；保留取数台账和进行中的开始时间。
- `dataservice/acquisition_need.go`、`websocket/plan_acquisition_v2.go`、`plan_runtime.go`：删除交接和重规划中的执行预算。
- `data-service/SKILL.md` 与 `defaults/system_experts/data_acquisition.md`：删除模型交接包含执行预算及因预算阻塞的说明。

普通 HTTP、OSM 获取和模型单请求超时、用户取消、模型 token/分片容量限制、原有智能体迭代及计划修正次数不属于此次五项执行预算，保持不变。`pausePlan` 仍用于不可恢复服务错误和真实连接错误，未删除这些职责。

## 已保存配置与暂停任务

实际配置加载器使用普通 `yaml.Unmarshal`，运行记录加载器使用普通 `json.Unmarshal`；旧预算字段自然被忽略，无需新增兼容代码或修改用户文件。正常手动继续沿用已有 `Controller.Resume`，从当前步骤继续，保留完成步骤及已绑定数据。本次未自动恢复任何真实任务，也未修改历史运行记录。

## 本地验证

执行一次局部检查：

```sh
go test ./internal/config ./internal/flowrun ./internal/agentruntime ./internal/dataservice ./internal/websocket -run 'TestAcquisition|TestV4AcquisitionSettings|TestV7ResumeHistoricalBudgetPause|TestV7ParallelPreparation|TestV7PreparesInput|TestV7CancelPausedPreparation|TestWrapToolWithErrorHandler|TestReplanInputProjectsCurrentFacts' -count=1
```

五个包均通过。夹具验证：旧配置仍能读取但不会重新输出预算字段；带旧预算字段和旧错误原因的暂停记录可继续；已完成步骤与已获取文件保留；数据准备及步骤不再附加执行 deadline；工具包装器连续 81 次本地调用全部执行；正常取消与工具错误映射不变。

验证仅使用临时目录、本地文件和模拟工具，没有模型、外部 API、联网或真实会话运行。

## 前端与交付

删除获取设置中的五项执行预算输入、前端预算类型与 `flow.budget` 事件处理，以及“调整执行预算”链接。保留普通中断的手动继续和停止操作；不修改旧日志中已经发生的错误记录。

主聊天区恢复原智能体卡片：带 `flow_run_id` 的 `workflowBlockId` 消息不再被过滤；运行中的合成“正在执行／查看详情”占位被移除，原思考、工具详情、用户确认和最终成果仍按各自组件显示。

相关前端7个文件、79项本地回归通过；TypeScript与修改文件ESLint通过。日志 `/tmp/enio2-chat-budget-removal-ui.log`、`/tmp/enio2-chat-budget-removal-types.log`、`/tmp/enio2-chat-budget-removal-lint.log`。包含系统提示词修正后的最终后端已通过标准构建发布，日志 `/tmp/enio2-no-execution-budget-final-sidecar.log`。未启动真实模型任务。
