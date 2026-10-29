# D50 前端与提案契约回归补充

## 结论
本记录覆盖前端门禁、旧 fixture 更新和补充 v2 提案集成。整体 `build.sh check`、真实 QGIS 与真实模型回放见 D50/D51 总记录。

## 改动摘要
- 新增 `layer-library.spec.ts`、`plan-data-v7.spec.ts`；旧 v6 用例改为历史计划只读，plan-run 迁移实际 v2 契约；live 流程检查允许系统单独安装时的缺少图层提示。
- 由后端代理将两项新 spec 加入 `build.sh` headless 清单。
- `plan_matrix_test.go` 使用真实临时 CSV/实际执行器验证 5 流程 × 3 方案 × user/retrieve；验证输出只由 produces 声明，不要求复制到 data；不插入取数步骤。
- 新增 `plan_v7_precheck_integration_test.go` 使用真实 `sessionPlanService.ProposePlan` 和临时持久化：先产出后使用、前向引用在持久化与执行前拒绝、缺失本地图层持久化为 needs_data 且 start 拒绝。
- 未改生产 Go 来迎合测试，未新增兼容层。

## 运行的命令与结果
| 命令/范围 | 结果 |
|---|---|
| `npm test -- --reporter=json --outputFile=/tmp/v7-ui-vitest-final.json` | 422 条中 416 通过、6 跳过、0 失败 |
| 图层引用入口最终增补后的 3 文件定向单测 | 12 通过（含新增第 417 个普通单测） |
| 计划卡最终定向单测 | 14 通过 |
| `npm run lint` / `npx tsc --noEmit` | 通过 |
| build.sh 所列全部 9 个 headless E2E spec | 12 通过，33.7 秒 |
| `layer-library.spec.ts` 深浅色截图增补后重跑 | 1 通过，21.2 秒 |
| `plan-data-v6.spec.ts`（历史只读） | 通过，已包含在单独 4 spec 运行 |
| Go API v2矩阵/文档/前向引用 | 通过，6.548 秒 |
| Go websocket `^TestV7(Proposal\|Missing)` | 3 通过，1.513 秒 |
| Python 内置流程两文件 | 11 通过，8.02 秒 |

headless 完整命令（`autocarto-web`）：

```sh
AUTOCARTO_PLAYWRIGHT_USE_DEV_SERVER=1 npx playwright test tests/e2e/backend-reconnect.spec.ts tests/e2e/diagnostics-page.spec.ts tests/e2e/services-page.spec.ts tests/e2e/guide-center.spec.ts tests/e2e/plan-run.spec.ts tests/e2e/layer-library.spec.ts tests/e2e/plan-data-v7.spec.ts tests/e2e/session-workspace.spec.ts tests/e2e/agent-profile-v2.spec.ts
```

Go 命令（`carto_agent`）：

```sh
go test ./internal/api -run 'TestGuidePlanMatrix|TestDocumentedMarching|TestV2PlanRejectsForward' -count=1
go test ./internal/websocket -run '^TestV7(Proposal|Missing)' -count=1
```

## 计数与耗时
上述不调用真实模型。浏览器测试在复制的前端和隔离 Go 后端上启动；新 UI spec 显式拦截 API/事件为夹具，不写真实用户配置、会话或项目。

## 跳过的测试及原因
普通单测 6 个 `api.real.test.ts` 是要求 `AUTOCARTO_RESOURCE_CONTRACT_BIN` 的 opt-in，用例不是失败。真实 QGIS 验收和五场真实回放由其他任务负责，不以界面夹具替代。

## 与方案不一致之处
提案入口实际位于 websocket 的私有 sessionPlanService，故补充提案预检集成测试放在 `internal/websocket`，API matrix 保留在 `internal/api`。v7 文档 JSON 示例省略了最终导出步骤却引用 `export.pdf`，文档计划测试显式添加其真实 producer 后验证，未放松生产依赖校验。

## 遗留问题
这里不能证明真实第三方服务、模型和 QGIS 全链路成功，也不能据此给出实际任务的成功率或速度提升。
