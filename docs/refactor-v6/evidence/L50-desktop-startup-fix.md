# L50 实际桌面启动复发修复与在线基础验证

日期：2026-10-28；承接 `b0545ea`。修复范围是实际开发启动路径及错误的就绪状态，不删除缺失能力依赖来绕过初始化。

## 上次修复的不足

用户确实在 17:10 重新启动了应用。新的 Tauri 和后端进程分别为 67479、67553，但它们仍继承两条指向旧 Eino 的 HOME/ASSETS 变量。现存 VSCode 终端自 10 月 25 日持续运行；删掉 `.zshrc` 导出不会清除已有终端进程的环境。此前隔离启动通过，不能证明这条实际桌面启动链已经恢复。

Tauri 先从旧资产路径推导项目根，又优先使用旧运行目录，最终仍找不到 route-cartography。17:11、17:12 的失败会话也继续写入旧目录。

此外，旧失败进程的 `/api/runtime/snapshot` 居然返回 Build/Plan ready=true。原逻辑默认 ready=true，只检查 Runtime 已记录的 ModeIssues；Runtime 根本没建成时 ModeIssues 反而为空。缓存还可能保留过期就绪状态。这使健康接口不能证明智能体实际可用。

## 代码修复

- Tauri 开发启动、没有显式 PROJECT_ROOT 时，识别 HOME/ASSETS 是否指向另一完整源码副本的标准 `autocarto_data` / `autocarto`。如果是，逐项切回当前编译源码项目，同时统一项目根，并输出旧→新路径日志。
- 不硬编码 Eino/enio2，不改变发行模式。自定义绝对路径、相对路径、独立资源库保持原语义；有意跨源码副本使用标准目录时，仍可显式指定 PROJECT_ROOT。
- 就绪状态改为检查实际可用 Agent 和模式错误；初始化失败返回 AGENT_UNAVAILABLE 及具体原因。快照每次刷新状态，既不缓存虚假成功，也不在恢复后残留失败。
- 保留原 fallback 语义及重建失败继续使用旧 Runtime 的行为；关闭的 Plan 不会被错误启用。

## 实际桌面恢复

1. 停止已失败的 Tauri 开发进程；原 Eino 源码与运行配置不修改。
2. 备份 enio2 已有会话，补回旧目录中新建的 02cf2f47 会话及 9ddfe02b 中追加的失败记录，合并最近工程列表。备份目录为 `autocarto_data/.backups/desktop-startup-2026-10-28T09-18-40-175Z/`。
3. 特意保留造成故障的两条旧环境变量，执行正常的 `npx tauri dev`。实际桌面日志显示已自动纠正两个目录。后端 PID 70064，日志确认 default Runtime 创建成功、334 个工具加载完成。
4. `ps` 仅提取相关路径、`lsof` 核对打开文件：后端的项目根、运行目录、资源目录、日志、instances.db 和 checkpoint_store.db 全部位于 enio2。
5. 原 QGIS 也由旧环境启动，仍等待已移走的旧端口文件。通过正常退出重开同一工程恢复连接；按现有开发启动规则同步 enio2 插件，额外备份被覆盖的插件文件。新 QGIS PID 70564，使用项目内托管 profile。原“测试 1.qgz”文件哈希前后完全一致，原生界面显示“AutoCarto AI 状态: 已连接”。

## 验证结果

- 6 个开发路径测试先红后绿，Rust `cargo test --lib -- --test-threads=1` 全部 20 项通过。
- 5 个新就绪状态场景先红后绿：无 Agent、恢复清理、保留旧 Agent、关闭 Plan、显式 ModeIssue 优先。
- `go test ./internal/websocket ./cmd/agent -timeout 120s` 与 `go test -p 1 ./... -timeout 120s` 全部通过。
- 实际后端状态：Build/Plan ready=true，mode_issues=null，service_phase=ready，真实 QGIS 实例 1 个。
- 数据获取目录接口 HTTP 200，9 个来源、23 个分类，diagnostics=[]。
- 通过实际生产 WebSocket，在新建验证会话 d391064f-d9d2-4d2f-a566-fb49b0877477 中发送“只回复连接测试成功，不调用工具、不下载、不修改工程”。17:27:34 发出，17:27:38 完成；持久化助手回复为“连接测试成功”，runtime_status=completed，无失败码、无工具调用。
- 初始观察连接未捕获最终 response 帧，监听脚本因此先记失败；随后核对生产会话的助手回复和 completed 状态确认成功，没有重新发送第二次模型任务。原始观察报告保留在本地备份目录，避免把监听器的结果当成产品失败。

脱敏报告：[L50-desktop-startup-check.json](L50-desktop-startup-check.json)。实际桌面和 QGIS 已保持运行，不需要用户再执行清理环境变量的命令；以后仍用原来的 `npx tauri dev`。

## 验证边界

本次已验证实际桌面启动、真实 QGIS 连接和主智能体基础回复。没有重放完整人口专题图，没有测试公网下载速度；基础恢复不能替代完整制图验收。
