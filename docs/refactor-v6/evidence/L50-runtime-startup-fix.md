# L50 新旧运行目录混用与初始化失败反馈修复

日期：2026-10-28。基于 `85898b5`，用户已授权修复。

## 结论与真实原因

15:55 的会话 `9ddfe02b-8a19-4859-a073-0ea6c4fe9692` 返回“Build 模式不可用：智能体未初始化”，没有工具调用或制图计划。这次请求尚未进入模型或下载阶段。

运行中的二进制来自 enio2，但继承的 AUTOCARTO_HOME、AUTOCARTO_ASSETS_ROOT 指向旧 Eino，进而推导出旧项目根目录。来源为用户 `~/.zshrc` 中两条固定导出。15:54:18 的日志已显示 ChatModel 初始化成功，随后因旧资源目录不存在 `route-cartography` 包，整个默认 AgentRuntime 创建失败；服务以受限模式继续启动。

因此，这次失败不是 OSM 代理或模型 schema 错误，也不能靠再次重试任务解决。缺少的包和数据分类目录都实际存在于 enio2；不应把新资源复制回旧项目来掩盖目录混用。

## 修改

- 备份后移除 `~/.zshrc` 的两条旧 Eino 路径导出。保留显式环境变量覆盖的正常语义，不改根目录解析优先级。
- enio2 本地运行配置的 `log.dir` 改为 `data/logs`，`data_dir` 改为 `data`，均相对项目默认运行目录 `autocarto_data`。
- 核对新旧凭据文件相同，保留 enio2 重构后的智能体配置。合并最近工程记录，并复制旧目录独有的最新失败会话（4 个文件，逐文件内容一致）；不覆盖其他 enio2 会话。
- 服务端保存启动及重建失败的具体原因，聊天和会话错误状态会显示该原因。成功安装 Runtime 后清除旧错误；重建失败时继续保留已有可用 Runtime。
- 新增 4 个回归测试，覆盖启动失败、聊天与会话错误传播、恢复后的错误清理、旧 Runtime 继续工作。

本地备份：

- `autocarto_data/.backups/runtime-path-2026-10-28T08-48-29-006Z/`（配置备份及 repair-report.json，目录权限 0700）。
- `/Users/zw/.zshrc.autocarto-path-backup-2026-10-28T08-48-29-006Z`（权限 0600）。

原 Eino 源码及运行配置未修改。运行配置与备份保持 gitignore，不提交凭据或用户会话。

## 验证

- 回归测试先复现“仅报告未初始化、丢失具体原因”，修复后通过。
- `go test ./internal/websocket ./cmd/agent` 通过。
- `go test -p 1 ./... -timeout 120s` 全量通过。
- 清除继承变量后启动新的 zsh 登录 shell，不再出现三项 AUTOCARTO 目录变量的旧绑定。
- 使用当前代码重新构建后端，以 enio2 资源、临时配置和独立端口启动。仅读取系统钥匙串中的既有凭据；所有模型地址改为本机拦截端点，实测模型请求数为 0。
- 约 3.85 秒启动；default Runtime 创建成功，334 个工具加载完成。运行状态接口确认 Build、Plan 均 enabled/ready，mode_issues=null、stale=false。
- `/api/data-sources` 返回 HTTP 200，9 个来源、23 个分类，diagnostics=[]。
- 验收进程已退出，临时配置及凭据副本已删除。脱敏证据见 [隔离启动报告](L50-runtime-startup-check.json)。

## 生效方式与验证边界

正在运行的旧进程仍保留启动时的环境；本次没有强制关闭应用或用户 QGIS。退出当前 `npx tauri dev` 后，在启动终端执行一次：

```sh
unset AUTOCARTO_HOME AUTOCARTO_ASSETS_ROOT AUTOCARTO_PROJECT_ROOT
cd "/Users/zw/研究生/QGIS-agent/enio2/autocarto-web"
npx tauri dev
```

此后正常使用原来的 `npx tauri dev` 即可。Tauri 构建脚本会构建并复制当前 Go 后端。

本次验证覆盖真实后端初始化与目录读取，未重放完整人口专题图任务，未调用真实模型、下载数据或启动 QGIS，不能据此宣称完整制图链路已通过。
