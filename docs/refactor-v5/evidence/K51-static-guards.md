# K51 — 静态守卫与迁移登记

- `TestV5RetiredExecutionMechanismsAbsent`：检查旧执行文件已删除，并扫描生产 Go、前端源码、默认方案、工具手册与工具定义，拒绝旧流程启动/预览、旧表单来源标记、角色填充策略和编译入口复活。
- 唯一旧来源标记例外为 `flow/legacy/input_choice.go` 的旧记忆解码；必须存在明确兼容登记。没有允许整个目录绕过扫描。
- `TestV5LocalRequestImplementationsExcludeModelAndTransportCalls`：直接检查指南解析/转换、计划校验/存储、数据来源查询和指南/计划 API 的导入及调用，拒绝直接构造模型与运输层请求。它是静态回归守卫，不代替数据服务的运行时零网络测试和端到端验收。
- `TestBuiltinGuidesParse`：四份内置 GUIDE 必须零条诊断；解析环境包含实际内置角色显示名，任何角色名泄漏也会令测试失败。
- removal ledger：关闭已经删除的编译别名、旧绑定 API、旧编译键升级桥；登记 GUIDE 转换、旧来源偏好迁移、旧方案忽略、历史 JSON 只读和隐藏下载器的删除条件。

验证：

```
go test ./internal/control -run 'TestV5|TestCompatLayerRequiresRemovalCriteria|TestCompletedRemovalsRequireEvidence' -count=1
go test ./internal/flow/... ./internal/api/... ./internal/config/... ./internal/settings/... ./internal/control/... -count=1
```

均通过。真实 QGIS、运行授权边界、三方案矩阵、前端 e2e 由同批次各泳道记录覆盖。
