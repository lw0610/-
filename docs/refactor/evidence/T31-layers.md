# T31 子范围：实例只读图层目录

本子范围在 T32 完成后单独执行。仅新增 `carto_agent/internal/api/handlers_instance_layers.go`、对应测试和 `/instances/{id}/layers` 路由；未改控制器、会话恢复或工具实现。

## 真实来源与接口

- 已核对 `project_state_impl.get_layers_info`：成功结果中的真实图层位于 `info.layers`，每个图层有 `id/name`，内部还可能有字段 `fields[].name`。不能对整个结果递归搜 name。
- 新增 `GET /api/instances/{id}/layers`，成功返回统一 Success `data={items:[{id,name}],instance_id}`。调用唯一固定的只读工具 `get_layers_info`，参数为请求实例 ID 和 `include_invisible:true`；桥接层实际发送 `target_instance_id`。
- 验证请求实例存在、状态 running、local bridge 已连接；复用 `authorizeInstanceToolInvoke(...,"get_layers_info","instance_layers",id)`，未知、隐藏或缺失 invoke scope 先拒绝，不向桥接发送调用。策略不可用、桥接离线、工具失败分别返回真实失败。
- 复用 `bridge.NewComputeToolInvoker`，15 秒上下文；保留桥接错误码和 provider diagnostics。未实现另一个工具发现来源，未信任插件上报 tools。
- 解析只访问明确的 `info.layers` / `layers`，以及 `data/result` 包裹（可含 JSON 文本或显式数组）；不遍历任意键，不从 fields、输出文字、其他嵌套对象提取图层。图层记录必须同时有非空字符串 id/name，按 ID 去重，同名不同 ID 保留。真实空数组返回 `items:[]`；无目录、错误结果或畸形条目返回 502，不伪装为工程无图层。

## 验证

- 定向 `go test ./internal/api -run 'Test(InstanceLayers|ExtractInstanceLayer)' -count=1` 通过：5 个测试函数，另覆盖 9 个可用性/策略子用例、多个解析路径和畸形结果。
- 同范围 `go test -race` 通过。
- 实例操作相关回归 14 个测试函数通过，涵盖已有注册、关闭、打开/创建工程、错误诊断和隐藏工具拒绝；未运行不相关的已知旧发现断言。
- HTTP→真实 local bridge provider→测试 WS 插件→HTTP 成功路径已验证，断言工具名、目标实例、include_invisible、Go 策略工具名及精确 `{id,name}` 投影。这里的插件为测试替身，未使用真实 QGIS，不作为真实端到端验证。
- `gofmt` 和 `git -c core.whitespace=cr-at-eol diff --check` 通过。

所有 Go 检查显式使用 enio2 的项目/资源根和独立临时 AUTOCARTO_HOME，WS/SQLite 夹具由 t.TempDir 创建。日志为 `/tmp/enio2-t31-layers-{tests,regression,race}.log`。未接触原 Eino、任何现有 autocarto_data 或已安装 QGIS/插件。
