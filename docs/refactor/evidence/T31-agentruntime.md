# T31 子范围：运行工厂生产依赖适配

本记录仅覆盖主代理委派的 `carto_agent/internal/agentruntime/` 子范围，不代表整个 T31 WebSocket/流程接入验收完成。没有修改 WebSocket、流程控制器或其状态文件。

## 已实现接口

保留原 `Builder` 的 Build/BuildActive 两个必须方法，新增独立可选 `FlowBuilder` 接口，现有普通任务 fake builder 不需要实现新方法。`Factory` 编译期断言实现以下三项：

```go
BuildRole(context.Context, string, *config.AgentConfigFile, string) (adk.Agent, error)
CallFlowJSON(context.Context, *config.AgentConfigFile, string) (string, error)
InvokeFlowTool(context.Context, string, map[string]any) (any, error)
```

- `BuildRole`：从 `buildDeepAgent` 提取共用 deepAgentConfig，原常规构造与流程角色使用完全相同的活动方案投影、工具目录、BaseTools、独立模型解析、provider 配置、RegistryGovernance、迭代预算及配置路径，再调用 T33 的 `deepagent.BuildRoleAgent`。flow_only 角色可以显式构造；角色授权和中间件继续由同一角色工厂处理。
- `CallFlowJSON`：查找本方案唯一已启用的 is_main 角色，使用其 provider（为空时为默认模型），执行一次温度 0 的模型调用。通过独立的空工具绑定及调用选项 Tools=[]、ToolChoiceForbidden 禁用工具；不改变共享模型。拒绝缺主角色/多主角色、空输入、缺模型、工具调用、空回包和非 JSON 文本。返回去掉首尾空白的完整 JSON，具体回执/条件结构由流程控制器验证。
- `InvokeFlowTool`：只允许 get_layers_info、layout_info，实际调用 Factory 已包装的 BaseTools，继续经过原观察器与错误映射；同时支持 EnhancedInvokableTool 的 ToolResult 文本 parts 和普通 InvokableTool。返回完整解码 JSON envelope，保留 success/data/output；success=false、非布尔 success、非法/空 JSON、重复注册、缺工具及传输失败转为错误。没有新建直连 Python 或桥接旁路。

按主代理追加要求，Plan 只读工具识别加入 flow_preview；即使 mode=all，flow_start 也不进入 Plan。新增筛选测试按真实 auto_mount=plan_main 元数据经过 selectPlanTools，在 readonly_only、readonly_plus、all 三种模式下均只选择 preview。

## 验证

在 enio2/carto_agent 下、使用新树独立忽略 runtime，移除外层继承的 AUTOCARTO_PROJECT_ROOT/AUTOCARTO_ASSETS_ROOT；无网络和外部模型调用。

| 检查 | 结果 |
|---|---|
| `go test ./internal/agentruntime -count=1` | **24 个顶层测试全部通过**，其中本次新增 6 项，另有三种 Plan 模式子例；0.529 秒，日志 T31-agentruntime.log |
| `go test -race ./internal/agentruntime -count=1` | **全部通过**；1.781 秒，日志 T31-agentruntime-race.log |
| 本次文件 gofmt、git diff --check | 通过 |

新增角色测试使用临时真实 SKILL.md、真实 RegistryGovernance、配置投影、技能中间件与 Eino Runner；只把模型响应替换为确定性探针。实际模型输入包含角色规则，工具包括授权的 get_layers_info 与实际 skill reader，没有未授权工具；角色独立 provider 被调用，默认模型未调用，输入方案未被修改。

新增模型测试检查选中重命名的主角色、默认 provider 回退、温度与工具选项、无效配置及错误输出。工具测试实际调用包装后的基础工具替身并解码 ToolResult，覆盖两种工具接口、桥接错误被映射后仍按失败处理，以及非法写工具从未执行。这里验证生产依赖适配，不宣称真实外部模型/QGIS 服务参加该 Go 测试；服务接入与完整流程恢复由主代理的 T31 验证覆盖。

日志位于 `autocarto/tests/golden/.runs/T31-agentruntime*.log`。没有开始其他任务卡。
