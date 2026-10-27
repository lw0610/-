# F25：瓦片加载的 Go 私有桥

## 调用契约

Controller 完成“密钥会进入 QGIS 工程”的明确确认后，通过绑定会话实例的 `toolsession` 上下文调用：

`InvokeAcquisitionTool(ctx, "data_load_tiles", {definition_path: 绝对路径, confirmed: true, authcfg: 可选认证编号})`

公开参数不含 Key。`authcfg` 不代替本次确认或天地图 tk 参数。`InvokeFlowTool` 仍只允许原来的两个只读观察工具。

## 边界

- Factory 严格检查参数，签发只存在于 Go context 中的一次性授权，绑定定义文件路径和会话 QGIS 实例。JSON 中的 `confirmed` 标志本身不是执行权限，模型不能通过参数伪造 context 授权。
- `bridge.Provider.InvokeTool` 拒绝调用方提供的 `__private_credentials`、缺授权、换路径、换实例、重放与已取消调用。即使活动实例改变，也只向授权时已绑定的实例派发。
- 目标实例连接确认后，才调用 `config.LoadTiandituAPIKey("")`。Key 仅放入最后一份 WebSocket 请求副本的 `__private_credentials.tianditu`，没有进入 BaseTool 参数、参数映射、模型 trace、来源定义或获取记录。原参数 map 保持不变。
- Bridge 的发送函数只序列化并发送消息，不记录参数；结果处理也不记录原结果。回到上游前，本次 Key 及 URL 编码变体再次从 output、错误、消息、图层路径和 diagnostics 中脱敏，私有字段被移除；密钥读取失败返回固定错误文案。
- Python 隐藏加载器、通用 source/日志脱敏以及 QGIS 工程中保存 URI 的明确确认由 F25-tiles.md 记录。工程本身保留 Key 是确认中明确告知的结果，不能把这称为无密钥工程。

## 验证

全部使用 fake Key、回环 fake bridge、新建临时运行目录和禁用 keyring；未读取真实配置或发送真实瓦片请求。

- `TestPrivateTile*` 覆盖无授权、已取消、路径/实例替换、重放、公开私有字段、未连接实例不读密钥、authcfg 不绕过 Key、最终副本注入、原参数不变、URL 编码 Key 与各输出字段脱敏、loader 错误不泄露。
- `TestFactoryTileSystemChannelKeepsCredentialsOutOfBaseToolArguments` 覆盖 Factory → BaseTool → 最终 bridge 的 context 授权传递、公开参数无凭据、无绑定实例拒绝、只读工具通道不扩大。
- 新增用例 `go test -race ./internal/middleware/bridge ./internal/agentruntime -run 'TestPrivateTile|TestFactoryTile' -count=1`：PASS（1.81 / 1.61 秒）。
- 两包普通完整测试：PASS（5.00 / 0.64 秒）。
- 首次 bridge 全包 `-race` 发现原有 discovery 注册表复制对象与注销/事件写入竞争，以及 provider 测试 hook 计数竞争，已交 F50 独立修复；不把新增用例通过说成该次全包竞争检查通过。
