# 新增 Skill / Tool 操作速查

## 新增系统 Skill

放这里：

```text
autocarto/skills/system/<skill-name>/SKILL.md
```

如果希望打包/初始化后也自带，再同步一份到：

```text
carto_agent/defaults/skills/<skill-name>/SKILL.md
```

最小结构：

```text
<skill-name>/
  SKILL.md
```

`SKILL.md` 最小模板：

```markdown
---
name: your-skill-name
description: 说明这个 skill 做什么，以及什么请求会触发它
kind: knowledge
---

# 标题

写流程约束、判断规则、交接格式、失败处理等。
```

常用 `kind`：

```text
knowledge   只提供流程/知识/约束
category    工具集合包，通过 tools 列出工具名
workflow    工作流说明或结构化 workflow
```

## 新增用户 Skill

放这里：

```text
autocarto/skills/user/<skill-name>/SKILL.md
```

用户 Skill 适合本机自定义，不建议作为默认内置能力提交。

## 新增默认 Agent 专属 Skill

如果 skill 只给某个 agent config 使用，放这里：

```text
autocarto/config/agent_configs/<config-name>/skills/<skill-name>/SKILL.md
```

例如默认方案：

```text
autocarto/config/agent_configs/default/skills/<skill-name>/SKILL.md
```

## 把 Skill 挂到 Agent

改当前运行时配置：

```text
autocarto/config/agent_configs/default/agent.yaml
```

如果你本地实际跑的是 `autocarto_data`，也同步：

```text
autocarto_data/config/agent_configs/default/agent.yaml
```

如果要改内置默认配置：

```text
carto_agent/defaults/agent_configs/default/agent.yaml
```

挂载位置示例：

```yaml
runtime:
  agents:
    orchestrator:
      skill_packages:
        - prompt-refinement
        - your-skill-name
```

常见挂载对象：

```text
orchestrator        任务理解、流程编排、路由规则
data_engineer       数据加载、字段、CRS、数据检查
spatial_analyst     空间分析、几何处理、网络分析
cartographer        符号化、布局、导出
scripting_specialist PyQGIS、复杂脚本、兜底自动化
planner             Plan 模式只读规划
```

## 新增系统 Tool

新增一个工具分两件事：

1. 注册工具：让运行时知道工具名、参数和执行入口。
2. 暴露工具：让某个 skill package 和 agent 能看到并调用它。

只新增 `.yaml` 或只写 handler 都不够。工具必须同时经过下面这条链路：

```text
handler 实现 -> handler wrapper 导出 -> tool YAML -> category skill 的 tools 列表 -> agent.yaml 挂载该 skill
```

### 1. 写工具实现

工具实现一般放在当前工具包的 handler 中：

```text
autocarto/tools/system/<tool-package>/handlers/*.py
```

如果多个工具包会复用同一段逻辑，放到共享实现：

```text
autocarto/tools/system/_shared/*.py
```

常见模式是：

```text
autocarto/tools/system/_shared/<domain>_impl.py        真实实现
autocarto/tools/system/<tool-package>/handlers/*.py    从 _shared 导入并导出 handler
```

例如制图工具常见链路：

```text
autocarto/tools/system/_shared/cartography_style_impl.py
autocarto/tools/system/cartography/handlers/style.py
```

如果工具包有 `handlers/__init__.py`，也把新 handler 加入导入和 `__all__`。这可以避免有的加载路径通过包级导出查找 handler 时找不到。

### 2. 写工具定义 YAML

工具定义放这里：

```text
autocarto/tools/system/<tool-package>/<tool-name>.yaml
```

最小结构：

```yaml
apiVersion: tool/v1
kind: Tool
metadata:
  name: your_tool_name
  display_name: your_tool_name
  runtime: qgis
  domain: <tool-package>
spec:
  description: 说明这个工具做什么
  parameters:
    - name: layer
      type: string
      required: true
      description: 图层名称
execution:
  handler: handlers.<module>.<handler_function>
  param_mapping:
    layer: "{{ layer }}"
```

注意：

- `metadata.name` 必须等于文件表示的工具名，也必须和 skill frontmatter 里的 `tools:` 条目一致。
- `metadata.domain` 必须等于工具定义所在目录名，例如 `cartography`、`print-layout`、`data-loading`。
- `execution.handler` 使用相对 handler wrapper 路径，通常以 `handlers.` 开头。
- 参数名要在 `spec.parameters` 和 `execution.param_mapping` 中保持一致。

同类工具示例：

```text
autocarto/tools/system/print-layout/layout_create.yaml
autocarto/tools/system/cartography/export_layout_pdf.yaml
autocarto/tools/system/data-loading/data_load_vector.yaml
```

### 3. 把工具加入 category skill 的 tools 列表

工具 YAML 只负责注册工具定义，不会自动让 agent 看到它。要暴露给智能体，还要把工具名写进对应工具集合 skill：

```text
autocarto/skills/system/<category-skill>/SKILL.md
```

例如 `cartography` 目录下的制图工具通常要加入：

```text
autocarto/skills/system/cartography-styling/SKILL.md
```

在 frontmatter 的 `tools:` 中加入工具名：

```yaml
---
name: cartography-styling
kind: category
runtime: qgis
enabled: true
tools:
  - cartography_set_style
  - your_tool_name
---
```

如果这里漏了，工具文件和 handler 即使都存在，agent 也可能看不到这个工具。

### 4. 确认 agent 挂载了该 skill package

工具只会暴露给挂载了对应 skill package 的 agent。检查：

```text
autocarto/config/agent_configs/default/agent.yaml
```

例如想让制图专家看到 `cartography-styling`：

```yaml
runtime:
  agents:
    cartographer:
      skill_packages:
        - cartography-styling
```

如果你本地实际运行的是 `autocarto_data` 下的配置，也同步检查或修改：

```text
autocarto_data/config/agent_configs/default/agent.yaml
autocarto_data/config/agent_configs/<config-name>/agent.yaml
```

如果要让打包/初始化后的内置默认配置也带上，需要同步内置默认 skill 或 agent 配置：

```text
carto_agent/defaults/skills/<skill-name>/SKILL.md
carto_agent/defaults/agent_configs/default/agent.yaml
```

### 5. 常见漏改点

```text
只写了 autocarto/tools/system/<package>/<tool>.yaml
但没把工具名加入 autocarto/skills/system/<category-skill>/SKILL.md 的 tools:

只在 autocarto/skills/system/... 修改
但当前运行时实际读取 autocarto_data/config 或 autocarto_data/skills 的副本

handler 在 _shared 里实现了
但没有在 autocarto/tools/system/<package>/handlers/*.py 中导出

handler wrapper 有导出
但 autocarto/tools/system/<package>/handlers/__init__.py 没有同步导入和加入 __all__

agent.yaml 里没有给目标 agent 挂载包含该工具的 category skill
```

### 6. 工具注册检查清单

以 `your_tool_name` 为例，提交前逐项确认：

```text
[ ] autocarto/tools/system/<package>/your_tool_name.yaml 存在
[ ] YAML metadata.name == your_tool_name
[ ] YAML metadata.domain == <package>
[ ] YAML execution.handler 指向 handlers.<module>.<function>
[ ] handler 函数存在，并能从对应 handlers 模块导入
[ ] 如存在 handlers/__init__.py，已导入该 handler 并加入 __all__
[ ] autocarto/skills/system/<category-skill>/SKILL.md 的 tools: 包含 your_tool_name
[ ] 目标 agent 的 skill_packages: 包含 <category-skill>
[ ] 如果当前运行用 autocarto_data/config，已同步对应 agent.yaml 或确认会自动生成
[ ] 重启或刷新 agent/runtime，避免旧工具目录缓存
```

## 新增用户 Tool

放这里：

```text
autocarto/tools/user/<tool-package>/<tool-name>.yaml
```

## 让 Skill 暴露 Tool

如果一个 skill 是工具集合包，写成 `kind: category`，并在 frontmatter 里列工具名：

```markdown
---
name: my-tool-package
description: 工具集合说明
kind: category
runtime: qgis
enabled: true
tools:
  - my_tool_a
  - my_tool_b
---

# 工具集合说明
```

然后在 `agent.yaml` 的对应 agent 下挂：

```yaml
skill_packages:
  - my-tool-package
```

这一步只负责“暴露”。如果工具本身还没有注册，仍然需要先完成“新增系统 Tool”里的 YAML 和 handler 链路。

## 常用目录总结

```text
autocarto/skills/system/                         系统 Skill
autocarto/skills/user/                           用户 Skill
autocarto/tools/system/                          系统 Tool 定义和 handler
autocarto/tools/user/                            用户 Tool
autocarto/config/agent_configs/default/agent.yaml 当前默认 Agent 配置
autocarto_data/config/agent_configs/default/agent.yaml 本地运行数据配置
carto_agent/defaults/skills/                     内置默认 Skill
carto_agent/defaults/agent_configs/default/agent.yaml 内置默认 Agent 配置
```

## 改完验证

Go 侧配置/skill 解析：

```bash
cd carto_agent
go test ./internal/deepagent/subagents ./internal/config
```

Python 侧工具定义和 category skill 引用检查：

```bash
python -m unittest autocarto.tests.test_definition_validation autocarto.tests.test_handler_tool_package_migration
```

手工排查时可以搜索工具名，确认它同时出现在工具定义、handler 导出和 skill `tools:` 列表里：

```bash
rg "your_tool_name|your_handler_function" autocarto/tools autocarto/skills autocarto/config autocarto_data/config
```

前端一般不需要改；如果页面缓存旧状态，重启 agent/runtime。
