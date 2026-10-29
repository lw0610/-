# L30 模型请求中空 schema 被转换为 null 的修复

日期：2026-10-28。实施基于预览改进提交 `6150a75`。

## 真实错误与归因

最新检查到的会话 `d594a94b-ed61-4c2b-85fa-6f36e8673d53` 于 15:37:47.934 发出“制作河南省各市人口统计专题图”请求，15:37:48.423 返回：

```
400 Bad Request: Invalid schema for function 'plan_propose':
null is not of types "boolean", "object"
```

会话 `tool_calls`、`carto_plans` 均为空，尚未执行下载或制图。运行中的后端构建信息为 `6150a757d437584c6c5e3d4c94eda663262a9944`、`vcs.modified=false`，与当时 HEAD 一致。不能将本次失败解释为用户没有重启或仍在使用更旧的后端；该提交确实只修改了预览，没有修复已发现的模型参数缺陷。

`plan_propose.yaml` 与 `plan_revise.yaml` 的 `facts.items.properties.value: {}` 表示允许任意 JSON 值。toolcontract、tool、skill 三层共用的深拷贝逻辑以 `len(src) == 0` 返回 nil，导致合法空对象序列化为 null，服务端拒绝整个工具定义。

## 修复

- 三处 `cloneMapAny` 仅在 `src == nil` 时返回 nil；显式空对象保留为空对象。
- 不修改流程文档，不收窄 facts.value 的类型，不通过删除工具规避问题。
- JSON Schema 库最终将该空 schema 序列化为布尔 `true`，含义与 `{}` 相同，仍接受任意 JSON 值。
- 新增公共工具目录和执行清单导出测试，检查空 schema、组合 schema、深拷贝隔离。
- 新增真实 YAML → 工具注册 → SkillAdapter → 项目模型客户端 → 本地 HTTP 请求体的回归检查，覆盖 334 个内置工具及两处自由值 schema。检查只进入 schema 位置，允许 default、enum、const、examples 中合法的 null 数据。

## 本地验证

- 修复前的新测试明确在 `plan_propose` 和 `plan_revise` 的 `parameters.properties.plan.properties.facts.items.properties.value` 失败，其余工具通过。
- 修复后同一请求体测试通过，原有 L30 嵌套定义测试通过。
- `go test -p 1 ./internal/toolcontract ./internal/tool ./internal/skill ./internal/llm -timeout 120s` 通过。
- `go test -p 1 ./... -timeout 120s` 通过。
- `go build -trimpath -o <临时目录>/carto-agent ./cmd/agent` 通过，构建信息确认 macOS arm64、CGO_ENABLED=1；未覆盖用户正在运行的二进制。
- 原隔离复现程序的结果由两处 `facts.value=null` 变为两处 `facts.value=true`。

## 最小在线验证

只读核对当前默认主智能体的模型配置：引用名称为 `qwen`，实际模型为 `deepseek-v4-pro`。使用修复后的真实 `plan_propose`、`plan_revise`、`data_find` 定义和合成的“仅回复 OK”提示，发出一次标准 chat/completions HTTP 请求；输出上限 128、总超时 60 秒，禁止重定向，不使用 SDK 自动重试，不执行模型返回的工具。

模型服务接受请求（HTTP 2xx，有效 choices 响应），返回工具调用数 0。四个既有配置和凭据文件在测试前后的哈希一致；未记录密钥。此结果证实当前服务接受修复后的三个工具 schema。它不是完整用户制图任务的回放，也不是所有模型服务的兼容性承诺。

## 使用与边界

重启应用后加载修复：退出当前 `npx tauri dev`，仍在 `autocarto-web` 目录运行同一命令。已有 Tauri 构建脚本会在复制后端前构建当前 Go 源码。本次未重启用户正在运行的服务，未改写真实会话、配置或工程。

请求格式验证不等于完整专题图任务验证，不能据此声称数据下载、人口数据匹配、制图和审查全部成功。
