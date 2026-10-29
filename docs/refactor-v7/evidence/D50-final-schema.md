# D50 最终 v2 工具契约验证

日期：2026-10-29。

最终检查中的三项失败分别核实如下：

1. `TestPlanCorrectionSuccessExitsAndNewRunsStartFresh`、`TestPlanCorrectionCountsOnlyInputFailures` 的测试样例宣称 v2，却仍使用 `steps.outputs`。迁移为 `steps.produces` 和成果 ID；无效样例采用真实的“输入 ID 不存在”，因为 v2 的未知能力应由规范化补齐，不再作为不可修正失败样例。保留真实 ADK 执行、错误计数、恢复和成功退出断言。
2. `TestL30RealToolSchemaReachesModelWithNestedContract` 确认了实际遗漏：`plan_propose.yaml`、`plan_revise.yaml` 的 `data.items`、`steps.items` 只有裸对象，模型可见 JSON Schema 缺失嵌套字段。补充数据来源、引用、需求、范围、输入、参数和产出属性；保持 ID、版本和能力可省略，无新增必填限制，不恢复 `find_id` 等 v1 字段。
3. `TestFactoryBuildRoleConstructsRealSystemDataExpert` 根据新取数职责调整：控制器负责来源查询/选择，执行专家不能再获取 `data_find`、`data_get`，仍必须获得 `data_register` 和实际检索工具。测试同时断言允许和禁止的工具。

验证命令：

```sh
go test ./internal/runtimecontrol ./internal/skill ./internal/agentruntime -count=1 -timeout 60s
```

结果：三个整包全部通过，耗时分别为 0.564、1.023、3.203 秒；命令总耗时 4.93 秒。日志 `/tmp/enio2-v7-final-contract-tests.log`。

工具定义属于本地 YAML 资源，无须仅为这两份定义重编 Go；已经运行的服务需刷新工具目录并重建 Agent，或用新进程启动后，模型才会拿到更新后的 Schema。
