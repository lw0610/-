# R73 编译、链接与环境缓存计数验收

日期：2026-10-22。对应 `04-编译与链接规范.md` §5；仅新增验收测试，未修改生产代码。

## 对既有断言的核对

- `internal/flow/link_test.go` 已覆盖数据变更、绑定变更不调模型、正文变更调模型、结构缓存不含运行路径。原模型使用非法 JSON，验收主要覆盖回退后缓存路径。
- `internal/flow/resource_stopgap_test.go` 已覆盖 `.dbf` 改变链接指纹，但直接调用指纹计算，不经过完整编译/链接。
- `internal/api/flow_environment_cache_test.go` 已覆盖单流程可用列表的底层环境加载/模型/内容哈希计数，以及目录元数据节流、方案立即失效和显式失效。没有统计编译器的 `LoadEnvironment` 回调次数，多流程误调用缓存包装的情况可能漏过。原单流程模型的步骤 JSON 缺少必填 `notes`，会回退；新专项使用完整 JSON 并拒绝回退诊断。

## 新增独立测试

- `internal/flow/performance_acceptance_test.go`：`TestR73CompileAndLinkCounters`。有效固定模型、完整 `Compile` 入口，覆盖大小变化、仅 mtime 变化、`.dbf` 变化/缺失/恢复、数据缺失/恢复、绑定设置/移除/恢复、正文变化。
- `internal/api/flow_performance_acceptance_test.go`：`TestR73AvailablePlansCountersAcrossMultipleFlows`。三个真实交通流程副本、真实工具/技能目录与 API 环境加载器；预编译后连续两次获取可用流程，同时统计回调次数与底层加载次数。

## 精确结果

第一次编译单步骤流程：模型 **2 次**，存在 `FLOW_RESOURCE_UNBOUND`。

| 变化（每行一次 Compile） | 模型增量 | 内容哈希增量 | 编译键改变 | 链接指纹改变 | 诊断结果 |
|---|---:|---:|---|---|---|
| 绑定有效目录 | 0 | 0 | 否 | 是 | UNBOUND 消失 |
| 数据文件大小改变 | 0 | 0 | 否 | 是 | 无 FILE_MISSING |
| 数据文件仅 mtime 改变 | 0 | 0 | 否 | 是 | 无 FILE_MISSING |
| `.dbf` 内容/大小改变 | 0 | 0 | 否 | 是 | 无 SIDECAR_MISSING |
| `.dbf` 删除 | 0 | 0 | 否 | 是 | SIDECAR_MISSING 出现 |
| `.dbf` 恢复 | 0 | 0 | 否 | 是 | SIDECAR_MISSING 消失 |
| 主数据文件删除 | 0 | 0 | 否 | 是 | FILE_MISSING 出现 |
| 主数据文件恢复 | 0 | 0 | 否 | 是 | FILE_MISSING 消失 |
| 移除绑定 | 0 | 0 | 否 | 是 | UNBOUND 出现 |
| 恢复绑定 | 0 | 0 | 否 | 是 | UNBOUND 消失 |
| 修改 FLOW.md 正文后保存 | 2 | 1 | 是 | 是 | 无 UNBOUND |

正文保存的清单内容哈希增量为 1：Store 保存流程时复制资源并更新文件元数据，资源内容哈希缓存失效。§5 对此要求的是模型增量大于 0；不把资源/绑定热路径的零哈希要求扩展到结构保存。测试不固定正文保存时的哈希次数，但明确记录观测值。

三个流程预编译：模型 **15 次**（每个流程 1 次整体 + 4 次步骤）；环境回调 **3 次**；底层环境实际加载 **1 次**；目录元数据扫描 **1 次**。

| 可用流程调用 | 返回流程数 | 模型增量 | LoadEnvironment 回调增量 | 底层环境加载增量 | 内容哈希增量 | 元数据扫描增量 |
|---|---:|---:|---:|---:|---:|---:|
| 第 1 次 | 3 | 0 | 1 | 0 | 0 | 0 |
| 第 2 次 | 3 | 0 | 1 | 0 | 0 | 0 |

两次之后底层实际加载总计仍为 **1 次**。这同时验证每次列表只取一次环境，而非每个流程调用一次环境包装。

## 命令和日志

工作目录：`carto_agent`。测试内部继续使用 `t.TempDir()` 隔离用户数据；外部环境显式覆盖继承的 Eino 路径。

```sh
env AUTOCARTO_PROJECT_ROOT='/Users/zw/研究生/QGIS-agent/enio2' \
    AUTOCARTO_ASSETS_ROOT='/Users/zw/研究生/QGIS-agent/enio2/autocarto' \
    AUTOCARTO_HOME=/tmp/enio2-r73-performance-home \
    go test -count=1 -v ./internal/flow ./internal/api \
    -run 'TestR73|TestLinkChangesDoNotRecompileOrPersistDataPaths|TestShapefileSidecarChangesLinkFingerprint|TestFlowEnvironmentCacheMetadataAndIsolation|TestAvailablePlansLoadsEnvironmentOnceAndHashesNoContent'
```

结果：**6 个 focused tests 全部 PASS**；flow 包选中测试耗时 0.996s，api 包选中测试耗时 1.415s。完整输出：`/tmp/enio2-r73-performance.log`。

## 验证边界

- 模型为有效固定 JSON，不调用外部模型服务；测试显式禁止 `FLOW_COMPILE_LLM_FAILED` 回退。
- 内容哈希计数来自 `resource.ContentHashCount()`，表示资源内容哈希实际文件读取次数；不计内存中的 JSON/元数据指纹计算，不代表零文件 I/O。链接仍会读取声明/锁定与检查文件元数据。
- 可用列表计数是在结构计划与资源摘要预热后观测；冷启动可以读 YAML、读资源内容、调用模型。环境测试固定时间以覆盖 1 秒节流窗口内的热路径；窗口到期与主动失效由本次一并执行的既有环境测试覆盖。
- Shapefile 使用元数据夹具，检验附属文件检查与指纹，不声称这些占位字节可被 QGIS 打开；真实 QGIS 契约由 R71 独立验收。
- 本次记录的单资源链接操作约 0.85–1.42ms；三流程热列表两次约 22.24ms、22.08ms。这些是本机单次测试观测，不设硬性耗时门槛，不外推大型数据或真实模型网络耗时。
- 本项只运行指定 focused tests；全项目构建与最终门禁由 R73 主验收统一执行。
