# T53 生产 WebSocket → 真实 QGIS 最小闭环

本子范围验收通过：直接指定流程和编排工具 `flow_start` 两种入口，都通过生产会话、角色工厂、ADK 阶段执行器、流程控制器及真实 QGIS 反向 WebSocket，完成加载隔离 GeoJSON → 导出新 GeoJSON 两步。

这不是一次真实 LLM 规划验收。编译模型与工具调用模型均为确定性、离线夹具，没有请求模型服务、读取云凭据或启用真实 GEE。正式编译器、正式角色工厂、工具 schema/参数映射/执行绑定、会话上下文注入、ADK runner、回执校验和持久化均使用生产实现。

## 文件与边界

- `carto_agent/internal/websocket/integration_flow_qgis_test.go`：新增 opt-in 集成测试；临时创建 v2 方案与两个 `flow_only` 通用角色，通过正式 `flow.Compiler` 编译两步 FLOW.md。角色使用实际 `data-loading` 工具包，main 使用 `flow-control`，通过 `agentruntime.Factory.Build` / `BuildRole` 创建。
- `autocarto/tests/integration/flow_qgis_ws_worker.py`：新增独立 offscreen QGIS worker；初始化 QGIS/内置 Processing，使用生产 `InstanceWSClient` 与 `QGISToolEngine`，仅接收 Go 下发的 authority execution manifest，不扫描本地 YAML 生成工具清单。
- HTTP/WS 使用测试自行创建的 `httptest` 随机端口；worker 从独立 HOME 的 `data/.port` 连接同一正式 `/ws?role=plugin` 入口。会话任务经真实 `/ws` 发送，包含 request/idempotency 字段；没有直接调用测试替代版 StepRunner 或 controller。
- QGIS 3.44.10-Solothurn；Go 下发 319 个绑定定义，Python 实际安装 317 个执行绑定，两个 quality 内部工具没有 Python 执行绑定属于预期。握手观察只读取测试 worker 的实际安装状态，没有恢复 Python 公开 discovery。
- 当前公共加载工具名是 `data_load_vector`，绑定 `handlers.loader.load_vector_handler`；任务中口述的 `load_vector_layer` 没有被重新造为别名。导出工具为 `data_export_vector`。
- 只新建上述测试/worker 和本记录；没有为了验收修改生产工具、控制器、WebSocket 或角色工厂实现。offscreen 加入 Processing 路径、注入正式 RegistryGovernance、读取增强 ToolResult 的文本部分和使用 `DeepServer.InjectToolRuntimeArgs` 都是验收装配，不是产品兜底。

## 实际结果

| 检查 | 直接指定流程 | 编排 `flow_start` |
|---|---|---|
| 状态 / 来源 | completed / direct | completed / orchestrator |
| 两步回执 | 各 1 次 attempt，验证成功 | 各 1 次 attempt，验证成功 |
| 真实 bridge 调用 | load 1、export 1、get_layers_info 1 | load 1、export 1、get_layers_info 1 |
| 要素、属性与坐标 | 2 个 Point，名称/数值/坐标一致 | 2 个 Point，名称/数值/坐标一致 |
| 输入文件 | 字节不变 | 字节不变 |
| 持久化 | run.json completed，编译 cache key 一致 | run.json completed，编译 cache key 一致 |
| 实时事件 | seq 单调，末次 16 | seq 单调，末次 20 |
| worker 收尾 | exit 0，已 Wait，无强制 Kill | exit 0，已 Wait，无强制 Kill |

导出使用文件名 `export.geojson`，返回的实际绝对路径位于各自正式会话的 `exports/` 下。回执采用实际工具返回的路径，控制器检查该文件存在、非空且为 `.geojson`；随后测试独立解析文件，核对两个要素的名称、数值与坐标。没有把请求中的路径当成已经存在的产物。

固定输入 SHA-256：`1b151193df5a6f25983d0ac0b4120f309819863fe1a46ed0a1905b20faa9e6e0`。

## 日志与持久证据

普通实际运行：`autocarto/tests/golden/.runs/T53-production-ws.log`，两入口通过，总测试包 4.376 秒。

最终带 `-race` 的实际运行：`autocarto/tests/golden/.runs/T53-production-ws-race.log`，两入口通过，没有 race 报告，总测试包 10.762 秒。最终工件目录：

- `autocarto/tests/golden/.runs/T53-production-ws-race/direct-135052330/`
- `autocarto/tests/golden/.runs/T53-production-ws-race/orchestrator-4252182775/`

每个目录包含 `result.json`、`events.json`、`bridge-calls.json`、`plan.json`、`worker-ready.json`、`worker-exit.json`、QGIS worker 日志、隔离 qgz/输入 GeoJSON、正式会话 `run.json` 与实际导出 GeoJSON。绝对运行路径只在这些本地证据中记录，测试源码不保存个人目录。

## 复现与默认行为

从新副本 `carto_agent` 运行：

```sh
env -u AUTOCARTO_PROJECT_ROOT -u AUTOCARTO_ASSETS_ROOT \
  AUTOCARTO_DISABLE_KEYRING=1 \
  AUTOCARTO_QGIS_PYTHON=/Applications/QGIS-LTR.app/Contents/MacOS/python \
  AUTOCARTO_FLOW_QGIS_EVIDENCE_DIR="$PWD/../autocarto/tests/golden/.runs/T53-production-ws-race" \
  GOPROXY=off \
  go test -race ./internal/websocket \
  -run '^TestProductionFlowWebSocketRealQGISLoadAndExport$' -count=1 -v
```

必须传入可用的 QGIS Python launcher；本机采用 QGIS 自带 `python` wrapper，直接启动 `python3.12` 缺少相应运行环境。未设置 `AUTOCARTO_QGIS_PYTHON` 时测试立即 Skip，不创建后端、端口、QGIS worker 或工程；该分支另有实际运行日志 `T53-production-ws-default-skip.log`。若不设置 evidence 目录，所有文件使用 `t.TempDir()`；设置时每次仍创建全新随机子目录。

测试只给自己通过 `exec.Command` 创建并持有句柄的 worker 发 Interrupt，必要时才 Kill，并等待退出；不按进程名搜索、终止或连接其他用户进程。最终两 worker 的 `worker-exit.json` 都是 `wait_completed=true, exit_code=0, forced_kill=false`。每次 HOME/PROJECT_ROOT/ASSETS_ROOT 通过测试作用域恢复，独立 HTTP server、插件连接、实例注册表和后端也在 cleanup 中关闭；没有部署桌面插件、打开原工程或写原 Eino。

本最小闭环不替代 T53 A1/A2 的制图图片/版面黄金比较，也不替代另一执行者的 Chrome UI 四路径验收；这些结果由各自记录及总 `T53.md` 汇总。

## 完整门禁后的独立补充

后续完整门禁暴露的 ScriptExecutor 终态发布竞态另有证据 `T53-executor.md`。该补充包含经复现确认的生产执行器修复；不改变以上 WebSocket 两步验收本身未修改生产代码、没有真实模型规划的边界。
