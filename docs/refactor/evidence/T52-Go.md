# T52 Go 子范围

本记录仅覆盖 config/deepagent/subagents/governance/control/skill/settings/extension 与相关 API/启动测试；WebSocket、flowrun、路由、图层 API 和工具资源下线由其他执行者负责。

## 删除前的测试保护映射

- `internal/workflow/{loader,graph_adapter}_test.go` 原来保护 workflow/v1 串行试点的解析、模板参数与工具顺序；该试点整包下线。新流程包的解析/引用/条件由 internal/flow 测试保护，顺序、失败策略、原文引用、中断恢复和产出验证由 internal/flowrun 测试保护；v1 方案中的 workflow_package 只允许作为输入产生退休警告，保留迁移专项测试。
- `internal/deepagent/workflow_agent_test.go` 原来保护按 workflow_package 创建串行代理、从多层目录发现包及工具桥调用；运行分支下线。统一角色创建、工具授权、目录来源、模型与中间件由 role_agent_test.go、agentruntime/flow_builder_test.go 和治理测试保护；知识执行器实际使用的通用工具调用/事件 helper 会迁入独立模块并保留其行为，继续由 knowledge_executor_test.go 等知识链测试覆盖。
- package_loader 的结构化 Workflow 字段测试改为证明退休 workflow.yaml 不再加载或执行；工具包的显式工具清单、配置、覆盖优先级与文件安全继续保留。
- 默认方案自动覆盖旧配置的断言改为新默认初始化与已有用户字段/未知包保留；旧默认文字断言改为 v2 角色类型、真实授权包及工具归属。v1 读取迁移及原始备份测试继续保留，不能通过恢复自动覆盖机制取得通过。

## 实际改动

1. 删除 `internal/workflow/` 全部生产代码与试点测试、`internal/deepagent/workflow_agent.go` 及对应测试；删除 `SkillPackage.Workflow`、运行时 `WorkflowPackage`、治理接口的 workflow 专属方法和创建分支。知识执行器仍使用的工具调用、索引和事件 helper 移到 `internal/deepagent/tool_runtime.go`，不再依赖另一套串行流程运行器。
2. `workflow_package` 仅在 config 的旧 YAML/JSON 输入结构中读取到私有字段，产生 `LEGACY_WORKFLOW_RETIRED` 警告；不写入 v2、不发布到 JSON 角色属性、不影响 kind/executor。`agent_config_v1_reader_migration` 支持窗口保留；已安装的旧用户说明包仍可读取。新增两项真实 YAML/JSON 回归检验这些边界。
3. 普通 Markdown/目录导入和 extension manifest 导入共同使用 `skill.ValidateToolPackageImport`，仅接受 `kind: category` 且明确含 `tools` 或 `tool_refs` 的新工具包。空包、纯正文、只写 `requires_tools` 的方法包以及 knowledge/workflow 新包均拒绝；退休的 `workflow.yaml` 也拒绝。保留原目录复制、引用/脚本保留、覆盖选择、原子安装、源目标重叠和符号链接防护测试。新增长度为 8 的校验表、4 类旧用户包原文件不变测试、manifest 第二资源不合法时零安装测试。有效工具包导入按已有运行守卫申请运行时更新，正在执行任务时继续延后重建。
4. 移除未调用的旧默认方案自动合并/覆盖辅助代码。新建配置目录缺少有效激活标记时优先 `default`，保留已存在且有效的用户激活记录；真实 ConfigManager 读写、公开列表和持久化 marker 的三场景测试覆盖空标记、无效标记、有效 creator 标记。API 的 `is_default` 本来由方案名 `default` 生成，`AgentConfigMeta` 没有另一个 `is_default` 字段。
5. 更新嵌入 creator 描述中的工具包术语；不覆盖已保存用户方案。`skill_creator` 的 v1 结构仍处于读取迁移窗口，创建器行为留待 T55，不借本卡扩大范围。
6. 默认方案、工具真实归属、GEE/统计/专题图 ADK 回归改为当前 category 工具包，保留真实模型可见工具、方法读取、任务返回、专属权限不外泄、调用 schema/参数映射的检查。显式给 main 分配 GEE 后，工具和同包方法均可读取；未分配时仍拒绝猜名绕过允许列表。旧授权基线中仅显式排除 T52 已退休的 `cartography_build_and_draw_from_tables`，并断言其不能继续授权，其余旧工具保留检查。
7. GEE worker 的上游归属文档引用同步到 `gee-remote-sensing`。GEE 边界可选 live 测试只引用新 `resources/system/data/中国行政区边界/templates/select_boundary.py`；不保留旧技能包资源 fallback。测试的 GEE 说明包改为 `gee-remote-sensing`，边界说明改为 `admin-boundaries`。
8. macOS 测试对真实工作目录采用物理路径，保持路径语义与顺序校验。schema revision 测试以真实输入 schema 变化为触发；policy revision 仍由已有独立测试覆盖。

## 测试密钥隔离

生产 `NewSecretsManager` 支持显式 `AUTOCARTO_DISABLE_KEYRING=1/true`，直接使用已有加密文件 fallback，不创建或调用 OS keyring 后端。测试验证新临时目录的加密文件、重新读取和本地删除。config/API/settings/control/deepagent/cmd 的 TestMain 强制开启该模式；WebSocket TestMain 与 build 门禁由主代理同步设置。现有注入 fake keyring 的后端回归保留。没有读取、输出或删除系统旧 keyring 条目；没有更改备份中的密钥。真实 OS backend service 源码为 `autocarto.ai`，Linux Secret Service label 为 `AutoCartoAI`。

## 已执行验证

- scoped 生产包编译通过。
- config、deepagent/subagents、control、skill、settings、extension、cmd/agent 完整定向回归通过，日志 `autocarto/tests/golden/.runs/T52-go-third.log`。
- capability、compute、plantools、integrations、geeruntime 定向全包回归通过，日志 `T52-go-peripheral.log`。GEE 为离线 mock EE 模块和真实独立 Python 进程；没有使用真实云凭据或提交云任务。
- 新默认选择、密钥本地隔离、旧输入退出运行层、实际工具包权限、普通导入拒绝、原子 manifest 拒绝、ADK 任务/方法读取与运行守卫等定向 `-race` 在 config/deepagent/control/settings/skill/extension 六包通过，日志 `T52-go-race.log`。
- 最终 `go test -p 1 ./... -count=1` 全量通过：38 个有测试包通过，其余包为 `[no test files]`，含真实 Python 计时测试、WebSocket 和 API。日志 `autocarto/tests/golden/.runs/T52-go-full-serial-final.log`。主代理将 Go 门禁同步为按包顺序执行；没有放宽功能断言。首次串行运行的 discovery 空主机夹具只申请 loopback 端口，无法保证 wildcard 空闲，已改为与被测 host 相同的接口申请端口，最终双 host 场景通过。
- 默认并发 `go test ./... -count=1` 两次执行：末次仅 geeruntime 的四项严格一秒启动/心跳测试失败，表现为超时前 stdout/stderr 均为空；其余包通过。相同 geeruntime 包定向全量通过（24.9 秒）。没有改这些测试阈值或生产超时。并行进程压力是待进一步验证的解释，不能据此声称默认并发全绿。日志 `T52-go-full-final.log` 保留失败证据。

## 未执行或由其他范围负责

- 真实联网 GEE/LLM live 测试依赖显式 opt-in，本卡未启用，不声称真实云验收。
- 本卡 Go 子范围没有运行桌面插件，也不以离线 ADK 测试代替 T53 生产 WebSocket → 真实 QGIS 端到端验收。
- WebSocket/flowrun、API 图层与路由、工具资源目录下线、Python/前端全量及最终 `build.sh check` / removal ledger 合并由对应执行者记录；本文件不把它们算作本代理独立完成。
