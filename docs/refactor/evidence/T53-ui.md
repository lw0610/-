# T53 A3 界面验收

日期：2026-10-21。A3 界面范围最终通过：最新静态生产构建连接真实隔离 Go 服务和 QGIS 实例，Chrome 用例 1 项通过（用例 11.5 秒，总计 12.8 秒）。完整 TypeScript 检查退出码 0。下文区分界面/协议验证与制图成果、模型调用、原生桌面操作，不把此前 fixture 回放计入真实联调。

## 隔离环境与实际链路

- 生产 `carto_agent/cmd/agent` 构建为 `/tmp/enio2-t53-agent`，监听 127.0.0.1:50151。
- 独立 `AUTOCARTO_HOME` 为 `/var/folders/3r/dc4g4r6d7fs7gndt3vgk_4p80000gn/T/enio2-t53-ui-f7304sgk`；PROJECT_ROOT/ASSETS_ROOT 指向 enio2，DISABLE_KEYRING=1。不读取已有凭据，不写原 Eino、已安装插件或既有 autocarto_data。
- 无模型密钥。真实编译器返回 `FLOW_COMPILE_LLM_FAILED` 警告并采用确定性回退，没有外部模型调用。方案保存真实返回“配置已保存、ChatModel 未初始化导致重建失败”；验证的是配置持久化，没有计为运行态重建成功。
- 独立 offscreen QgsApplication 创建临时工程与点图层，通过正式 `InstanceWSClient`/`QGISToolEngine` 连接生产反向 WS。QGIS 3.44.10；authority 下发 319 个执行定义，实际建立 317 个绑定，两个质量审查内部能力没有插件执行绑定。没有安装插件或复用桌面工程。
- 真实 GET `/api/instances/t53-ui-real-qgis/layers` 返回工程中的“真实隔离核验点”及实际图层 ID。
- 浏览器只注入桌面 runtime 地址发现；HTTP/WS 连接真实服务，没有 `route.fulfill` 或伪造流程事件。原生文件选择器与系统成果查看器未实际启动。
- 前端在独立临时副本完成 Next 优化静态构建，使用项目依赖，避免与根完整检查争用 `.next/out`。最终构建包含下列四项修复。

## 四条使用路径与三个页面

| 范围 | 实际操作与断言 | 证据 |
| --- | --- | --- |
| B 导入 | 从真实目录导入 FLOW.md 与 CSV；缺失相对 GeoJSON 显示诊断；点击“选择文件夹”绑定实际目录后错误消失；真实计划资源路径一致且 exists=true | [导入诊断](../../../.runs/T53-ui/enio2-t53-import-diagnostics.png)、[绑定后](../../../.runs/T53-ui/enio2-t53-resource-binding.png) |
| C 自写 | 从模板新建并编辑为两个确认步骤；真实 PUT 保存、解析、预览输入/步骤；导出 ZIP 实际落盘且有 PK 签名；导入公共 Markdown 并预览真实文本 | [输入预览](../../../.runs/T53-ui/enio2-t53-authored-inputs.png)、[公共资源](../../../.runs/T53-ui/enio2-t53-public-resource.png) |
| D 方案 | 角色只显示工具包，基础 system 包禁选；切换实际几何处理包选择；启用两个用户流程；等待当前 PUT 完成后 GET/data 确认 v2 tool_packages、flows.enabled 落盘且正常输出无 skill_packages；流程链接带正确 flow/scheme | [方案](../../../.runs/T53-ui/enio2-t53-scheme.png)、[真实 HTTP 结果](../../../.runs/T53-ui/enio2-t53-ui-http-evidence.json) |
| A 对话 | 流程中心试运行跳转初始化流程；实际内置表单显示纸张/质量选项，空可选路径/标题保持空；自写流程提交真实 task，完成两次人工确认，进度 2/2、服务端 completed；任务 socket 全程没有关闭，终态无后端断连横幅 | [内置表单](../../../.runs/T53-ui/enio2-t53-builtin-form.png)、[等待确认](../../../.runs/T53-ui/enio2-t53-live-waiting.png)、[完成](../../../.runs/T53-ui/enio2-t53-live-completed.png) |

流程中心、方案设置、工具中心均实际打开检查。真实工具手册以“排版”搜索得到“按版式一次排版”，见[工具手册截图](../../../.runs/T53-ui/enio2-t53-tool-handbook.png)。截图已打开复核，未发现所检查内容的遮挡或溢出。方案修改导致旧计划过期，重新体检后才试运行，符合预期约束。

## 最终真实任务

- `flow_run_id`：`fr-20261021-122322-6fc3fab9b700`
- `session_id`：`5147c394-cd50-4494-93a0-c0995409c546`
- 最终 `status=completed`，`s1/s2` 均 completed，`event_seq=20`。
- 实际 task 的 request_id/idempotency_key 相同，顶层与 data 一致；`data.flow.id=ui-authored`。输入仅包含对话原文引用、A4 横向、false 三项；空可选目录和标题没有混入 false 或空路径值。
- 保存的 14 个选定 WS 帧包含 task、task_ack、flow.started、两次 interrupt/interrupt_response、步骤完成和 flow.finished。`task_socket_closes=[]`、`pageerrors=[]`。
- [真实运行文件](../../../.runs/T53-ui/run.json)、[WS 帧](../../../.runs/T53-ui/websocket-frames.json)、[HTTP/浏览器断言](../../../.runs/T53-ui/enio2-t53-ui-http-evidence.json)。

这条最小两步确认流程验证界面、人工交互、正式任务协议和持久化，不代表内置制图流程的地图成果验收；A1/A2 真实制图执行由总验收链单独记录。成果路径展示与 run.json 磁盘内容已检查，原生打开成果未纳入本轮。

## 真实联调发现与修复

1. **真实嵌套目录为空。** `/api/tools/catalog?scope=global` 的 Success data 是 `{catalog:{items},scope,...}`，流程中心此前读 data.items，旧 fixture 顶层响应掩盖问题。`flowApi.capabilities` 复用正式 `api.getToolCatalog('global').tool_infos`；API 测试增加生产嵌套响应，system-smoke 同步读 catalog.items。定向 4 项通过。
2. **task 缺少必需身份。** sendTask 未携带 Go task 契约要求的 idempotency_key，任务在执行前被拒绝。现为新任务生成 request_id/idempotency_key，顶层与 data 同步；同次恢复重发保持身份，不同任务使用不同身份。WebSocketClient/hook 定向 16 项通过，真实服务已接收并执行最终任务。
3. **null 可选默认值显示 false。** 生产 API `default:null` 被 inputDefaults 初始化为文本 false。现仅布尔输入回退 false，其余 null 保持空并提交时省略。模型/组件 14 项通过，最终真实表单和实际发送对象均断言空可选值未发送。
4. **会话变化触发连接重建。** Provider 连接 effect 依赖随会话集合变化的 attachment callback，导致完成时断连横幅。frontend_runtime 独占修复为最新 callback ref；新增两项回归在旧实现实际失败、修后通过，相关 6 文件 47 项通过。本轮最终浏览器额外断言 task socket 关闭数为 0 且终态断连横幅消失。

初期验收脚本也修正了自身等待条件：新建详情到达后再编辑；保存元数据不比较字段顺序；ZIP 与方案保存等待当前请求真正完成；方案变化后重新体检。没有为这些脚本问题放宽产品协议或更改生产行为。

## 验证记录与重跑入口

| 检查 | 结果与覆盖 |
| --- | --- |
| 最新独立 Next 优化静态构建 | 成功，包含四项最终修复，[日志](../../../.runs/T53-ui/enio2-t53-isolated-web-build.log) |
| 最新 Chrome 真实服务用例 | 1 passed，11.5 秒/总 12.8 秒，[日志](../../../.runs/T53-ui/enio2-t53-ui-static.log) |
| 最终完整 TypeScript 检查 | `./node_modules/.bin/tsc --noEmit --pretty false`，exit 0，[输出](../../../.runs/T53-ui/enio2-t53-final-tsc.log)、[退出状态](../../../.runs/T53-ui/final-verification.json) |
| 目录/发送/null 定向回归 | 4/16/14 项通过；[目录](../../../.runs/T53-ui/enio2-t53-catalog-test.log)、[发送](../../../.runs/T53-ui/enio2-t53-task-contract-tests.log)、[null](../../../.runs/T53-ui/enio2-t53-null-default-tests.log) |
| 最终完整 build.sh check | 主代理报告 exit 0，含 341 前端、391 Python 测试及全部构建，详见 [T53.md](T53.md)；此目录中较早的 336 测试/lint 日志保留历史时序，不作为后续修复的最终全量结果 |

重跑入口为 `autocarto-web/playwright.refactor-ui.config.ts` 与 opt-in `tests/e2e/refactor-ui-live.spec.ts`。使用独立服务，设置 `AUTOCARTO_REFACTOR_UI_LIVE=1`、`AUTOCARTO_TEST_RUNTIME_PORT=50151`；已有静态构建时设置 `AUTOCARTO_REFACTOR_UI_STATIC=1` 与 `AUTOCARTO_REFACTOR_UI_EXPORT_DIR`。未设置 live 开关时明确跳过，不误连用户环境。静态服务只服务已有导出目录，不杀占用端口进程。

## 持久证据与清理

上述链接的根目录是 **`/Users/zw/研究生/QGIS-agent/enio2/.runs/T53-ui/`**，不是 `autocarto/tests/golden/.runs/T53-ui/`。截图、HTTP/WS 数据、运行文件、构建/测试日志均已复制到该项目根目录；`before-fixes/` 留存 null 文本与断连横幅原状。证据不依赖 /tmp 存活。

本轮自己启动的 Go 服务 PID 78036、QGIS worker PID 90867 已停止；复核两个 PID 均不存在，50151/50152 均无监听。未关闭用户服务。此 UI 子范围没有修改 Go、工具资源、既有运行数据或原 Eino。
