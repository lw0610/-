# D50 目录重复扫描定位与修复

日期：2026-10-29。

## 复现与证据

整套 Go 测试中的 agentruntime、api、websocket 包长时间占用 CPU。对本次测试进程取栈后，热点是：

`skill.Info → GuideBackend.List → GuideStore.List → Get → GuideEnvironmentFromLibrary → layers.List → 每个图层的 Validate → resource.NewDefaultLibrary → 全资源目录扫描`。

不是模型无出口，也未发现 JSON 递归。Go 主测试进程在子进程退出后仍消耗 CPU，macOS `sample` 显示 `cmd/go/internal/test.computeTestInputsID → EvalSymlinks`，即海量重复文件访问进一步放大了测试缓存输入统计的成本。

证据保存在本次临时日志：

- `/tmp/enio2-v7-go-all.log`：原执行和 SIGQUIT 栈。
- `/tmp/enio2-v7-go-driver-sample.txt`：Go 主测试进程采样。
- `/tmp/enio2-v7-api-hang.log`、`/tmp/enio2-v7-websocket-hang.log`：25 秒超时定位，均落在资源扫描。

## 修改

- 图层目录一次 `List` 或 `SaveBatch` 共用类别、默认样式查找表。
- 流程环境已经读取同一个资源库时，将该次结果传给图层校验，避免重新创建并扫描资源库。
- 流程摘要列表只读取元数据、启停状态、转换报告。详细资源解析仍在实际打开或使用选中流程时执行。
- 没有引入跨请求缓存。下一次请求会重新读取类别、样式和流程修改。

## 验证

- `go test ./internal/layers ./internal/flow -count=1 -timeout 60s`：通过。
- 新测试 `TestCatalogUsesSuppliedStylesAndReloadsOnNextRead`：确认调用方样式列表有效，下一请求缺少样式或修改类别后会即时反映。
- 新测试 `TestGuideSummaryDoesNotResolveUnselectedResources`：未选中的资源目录不可用时仍能列出流程摘要，保留标签、启停状态及转换报告。
- `TestFactoryDefaultRuntimeReadsAllBuiltinGuidesAndAssignedManual` 与 `TestFactoryGuideReadsReflectUserChangesWithoutRuntimeRebuild`：通过，合计 3.356 秒。
- 单个真实 `marching-route` 技能读取样例测试时间从 1.083 秒变为 0.763 秒；含构建的命令总耗时分别为 1.94 和 2.00 秒。该单样例不能代表完整模型制图任务的速度。
- 三个原慢包以 `-count=1` 运行，在 29.38 秒内返回，其中 API 包通过（14.139 秒）；另两包有两项同期执行契约变更的断言失败，已交由对应代码负责者处理。未修改断言掩盖失败。
- 追加启用 Go 默认测试缓存的同组三包检查，31.64 秒内结束：API 15.930 秒、WebSocket 26.281 秒通过；同期事件缓冲修复已被该轮覆盖。此轮仅系统取数专家工具授权断言待对应负责人确认，没有挂起或残留测试进程。
