# Eino 项目工具与智能体治理手册

> 版本：v1（基于当前代码实现整理）  
> 目标：后续工具与智能体管理可直接按本文档执行，不依赖口头传达。

## 0. 快速入口（后续重点）

后续治理默认只做两件事：**新增工具** 与 **注册智能体配置**。  
推荐顺序固定为：`新增工具 -> 绑定到 skill 包 -> 分配到 agent 配置 -> 激活配置 -> 验收`。

### 0.1 新增工具（最短路径）

1. 准备工具定义（`tool/v1` 或 `tool_contract/v1`），至少包含：
   - `metadata.name/domain/runtime`
   - `spec.description/parameters`
   - `execution`（`handler` 或 `handler_file+handler_function` 或 `backend_tool`）
   - `policy.visibility`（对外发布时设为 `public`）
2. 将工具定义放入生效目录（见第 8 章目录优先级）。
3. 用 `validate_skill_definition` 做结构校验（推荐强制）。
4. 通过 `save_skill_to_package` 绑定到对应 skill 包。
5. 验证生效：
   - `GET /api/runtime/descriptor` 中 `execution_manifest` 包含该工具
   - `GET /api/tools/catalog?scope=active` 可见性符合预期
   - 实际触发一次 `tool_invoke`，确认收到 `tool_result` 终态

### 0.2 注册智能体（最短路径）

1. 用 `create_agent_config` 新建配置（建议 `clone_from=default`）。
2. 用 `assign_skill_packages` 把 skill 包分配到：
   - `runtime/global` 或 `runtime/agent`
   - 如需规划能力，再分配 `plan/global` 或 `plan/agent`
3. 激活配置：`POST /api/agent-configs/{name}/activate`。
4. 验证生效：
   - `GET /api/runtime/snapshot` 检查 `runtime_active_agent_config`
   - `GET /api/tools/catalog?scope=active` 检查工具集是否符合该配置

### 0.3 发布前硬性检查（必须通过）

1. 目标工具存在于 `execution_manifest`，且执行绑定字段完整。
2. 目标工具 `visibility` 与发布范围一致（`public` 或非公开）。
3. 目标 agent 配置已激活，不是“仅保存未切换”状态。
4. 至少有一次真实调用闭环：`tool_invoke -> tool_result(terminal_state)`。

## 1. 项目总览

本仓库由三部分组成：

1. `autocarto/`：QGIS 插件（Python），负责接入 QGIS 环境、执行工具、通过反向 WebSocket 与后端交互。
2. `carto_agent/`：Go 后端控制面，负责配置治理、工具目录/能力下发、会话与智能体运行编排。
3. `autocarto-web/`：前端（Next.js + Tauri），负责配置与运行态可视化管理。

核心设计原则：

1. **工具执行绑定以后端 authority 为准**：插件不再本地扫描 skill 目录做执行绑定。
2. **运行时发现统一走 descriptor**：插件先读端口，再拉 `/api/runtime/descriptor` 获取权威执行契约。
3. **工具公开有策略过滤**：只有 `visibility=public` 的工具会进入 public catalog 并被对外暴露。

---

## 2. 关键目录与文件

### 2.1 Python 插件侧（`autocarto/`）

| 文件 | 作用 |
|---|---|
| `autocarto/autocarto_plugin.py` | 插件入口，初始化工具引擎与 WS 客户端 |
| `autocarto/core/service_port_reader.py` | 读 `AUTOCARTO_HOME/data/.port`，请求 runtime descriptor |
| `autocarto/core/instance_ws_client.py` | 反向 WS：`instance_connect/heartbeat`、接收 `tool_invoke`、回传 `tool_result` |
| `autocarto/core/tool_engine/engine.py` | 从 descriptor 读取 `execution_manifest` + `tool_catalog` 并注册执行 |
| `autocarto/core/tool_engine/definitions.py` | 将 manifest 条目转为插件可执行定义 |
| `autocarto/core/tool_engine/executor.py` | 按 `handler / handler_file / backend_tool` 执行工具 |
| `autocarto/core/tool_engine/runtime.py` | 工具执行结果规范化、主线程调用保护、JSON 序列化兜底 |
| `autocarto/core/session_manager.py` | 会话上下文与输出目录联动 |
| `autocarto/tools/system/_shared/output_manager.py` | 输出目录管理、路径归一化、防越界 |
| `autocarto/tools/system/_shared/input_path_resolver.py` | 输入路径候选解析（project/cwd/autoc 子目录） |

### 2.2 Go 控制面（`carto_agent/`）

| 文件 | 作用 |
|---|---|
| `carto_agent/cmd/agent/main_deep.go` | 后端主入口，装配 API、WS、runtime、工具/技能/配置服务 |
| `carto_agent/internal/api/routes.go` | REST 路由总表 |
| `carto_agent/internal/api/handlers_runtime.go` | `/api/runtime/snapshot/services/descriptor` |
| `carto_agent/internal/control/discovery_descriptor.go` | descriptor 结构与 ready/block/degraded 判定 |
| `carto_agent/internal/config/paths.go` | `AUTOCARTO_HOME`、`AUTOCARTO_ASSETS_ROOT`、目录解析 |
| `carto_agent/internal/config/agent_config_manager.go` | `agent.yaml` 加载、校验、切换、持久化 |
| `carto_agent/internal/config/agent_config_crud.go` | creator-tools 对 agent 配置的增改与 skill 分配逻辑 |
| `carto_agent/internal/tool/types.go` | `tool/v1`/`tool-package/v1` 定义与校验 |
| `carto_agent/internal/toolcontract/model.go` | `tool_contract/v1` 定义（authority canonical contract） |
| `carto_agent/internal/tool/loader.go` | authority 工具加载，空目录时回退 `defaults/tool_contracts` |
| `carto_agent/internal/control/asset_catalog_service.go` | 工具目录投影、public 策略过滤、catalog 合约输出 |
| `carto_agent/internal/toolcatalog/catalog.go` | catalog contract、可用性/降级原因计算 |
| `carto_agent/internal/creatortools/tools.go` | creator-tools 后端实现 |
| `carto_agent/internal/middleware/bridge/provider.go` | 插件反向 WS provider（`tool_invoke/tool_cancel/tool_result`） |
| `carto_agent/internal/websocket/runtime_tool_args.go` | 工具调用前自动注入 `session_id/project_path/session_dir...` |

### 2.3 前端侧（`autocarto-web/`）

| 文件 | 作用 |
|---|---|
| `autocarto-web/src/lib/api.ts` | REST API 类型与客户端封装（runtime/catalog/config 等） |
| `autocarto-web/src/lib/backendRuntime.ts` | 运行时地址探测（含 `/api/services/ports`） |
| `autocarto-web/src/stores/*` | 运行态/连接态状态管理 |

---

## 3. 端到端调用链（工具执行主链路）

1. Go 进程启动后写入运行端口：`AUTOCARTO_HOME/data/.port`。
2. 插件读取 `.port`（`service_port_reader.py`），请求：
   - `GET /api/runtime/descriptor?contract_version=v1`
   - Header：`X-AutoCarto-Contract-Version: v1`
3. descriptor 返回：
   - `execution_manifest`（执行绑定清单）
   - `tool_catalog`（公开工具目录）
4. 插件用 `execution_manifest` 建立执行注册（handler/algorithm 等），并维护对外 `tool_catalog` 视图。
5. 插件发起反向 WS 到 descriptor 的 `ws_url?role=plugin`。
6. 后端发 `tool_invoke`，插件执行后回 `tool_result`。

---


## 4. 工具定义规范与调用方式

## 4.1 支持的工具定义版本

1. `tool/v1`（当前大量 YAML 在用）
2. `tool-package/v1`（兼容）
3. `tool_contract/v1`（authority canonical contract）

authority 加载策略：

1. 优先读取 `ResolveBuiltinToolsDir()` + `ResolveUserToolsDir()`。
2. 若为空，回退到 `carto_agent/defaults/tool_contracts`（内置默认）。

## 4.2 `tool/v1` 关键字段（治理必须掌握）

```yaml
apiVersion: tool/v1
kind: Tool
metadata:
  name: canvas_set_scale
  domain: canvas-navigation
  runtime: qgis
spec:
  description: 设置地图比例尺
  parameters:
    - name: scale
      type: number
      required: true
execution:
  handler: handlers.canvas.set_scale_handler
  param_mapping:
    scale: "{{ scale }}"
policy:
  visibility: public
```

执行绑定字段：

1. `execution.handler`：模块函数路径
2. `execution.handler_file` + `execution.handler_function`：显式文件函数
3. `execution.backend_tool`：后端工具/算法 ID
4. `execution.cli`：CLI 执行（主要在 Go skill/backend 侧）

## 4.3 参数映射与模板（skill/tool 引擎）

`param_mapping` 支持模板表达式：

1. `{{ var_name }}`
2. `{{ auto_output_path("xxx.gpkg") }}`
3. `{{ resolve_layer(layer_ref) }}`

模板变量来源包括：

1. 调用参数本身
2. 运行时注入参数（如 `session_id`、`project_path`、`instance_id`）
3. 会话上下文派生（如自动输出路径）

## 4.4 公开策略

只有 `visibility=public` 的工具会进入 public catalog。  
`hidden/internal` 不对外发布。

---

## 5. Creator Tools（新增工具/注册智能体主入口）

这 9 个工具是“对话式治理工具与智能体”的主入口。

| 工具名 | 主要参数 | 用途 |
|---|---|---|
| `list_skill_packages` | 无 | 列出可见 skill 包 |
| `create_skill_package` | `package_id,name,description,kind,runtime,body` | 创建新 skill 包（含 SKILL.md） |
| `validate_skill_definition` | `definition`(object, required) | 校验 skill/tool 定义结构 |
| `save_skill_to_package` | `package_id`(req), `definition`(req) | 将定义保存到包并更新 tool_refs |
| `save_skill_script` | `package_id,script_path,content`（全 req） | 保存包内脚本文件 |
| `list_agent_configs` | 无 | 列出 agent 配置方案 |
| `create_agent_config` | `config_name`(req), `clone_from`, `config_data` | 新建配置（可克隆） |
| `update_agent_config` | `config_name`(req), `config_data`(req) | 覆盖更新配置 |
| `assign_skill_packages` | `config_name,mode,target,agent_name,skill_packages` | 分配 skill 包到 runtime/plan 的 global 或 agent |

推荐调用顺序（新增工具）：

1. `validate_skill_definition`
2. `create_skill_package`（首次建包时）
3. `save_skill_to_package`
4. `save_skill_script`（有脚本才需要）
5. `list_skill_packages`（确认可见）

推荐调用顺序（注册智能体）：

1. `create_agent_config`
2. `assign_skill_packages`（按 runtime/plan 分层绑定）
3. `update_agent_config`（仅在需要整体覆盖时）
4. 激活配置（REST：`POST /api/agent-configs/{name}/activate`）

`assign_skill_packages` 约束：

1. `mode`：`runtime` 或 `plan`（空默认 `runtime`）
2. `target`：建议仅用 `global` 或 `agent`
3. `target=agent` 时必须给 `agent_name`

调用示例（抽象）：

```json
{
  "tool_name": "assign_skill_packages",
  "args": {
    "config_name": "my_config",
    "mode": "runtime",
    "target": "agent",
    "agent_name": "spatial_analyst",
    "skill_packages": ["overlay-analysis", "buffer-proximity-analysis"]
  }
}
```

新增工具最小调用模板：

```json
{
  "tool_name": "save_skill_to_package",
  "args": {
    "package_id": "canvas-navigation",
    "definition": {
      "apiVersion": "tool/v1",
      "kind": "Tool",
      "metadata": {
        "name": "canvas_set_scale",
        "domain": "canvas-navigation",
        "runtime": "qgis"
      },
      "spec": {
        "description": "设置地图比例尺",
        "parameters": [
          {"name": "scale", "type": "number", "required": true}
        ]
      },
      "execution": {
        "handler": "handlers.canvas.set_scale_handler",
        "param_mapping": {"scale": "{{ scale }}"}
      },
      "policy": {"visibility": "public"}
    }
  }
}
```

注册智能体最小调用模板：

```json
{
  "tool_name": "create_agent_config",
  "args": {
    "config_name": "spatial_ops_v1",
    "clone_from": "default"
  }
}
```

---

## 6. 智能体配置规范（`agent.yaml`）

配置文件结构（`version: "1.0"`）：

1. `meta`
   - `default_execution_mode`: `build` 或 `plan`
   - `description`
   - `plan_enabled`
2. `runtime`
   - `global.skill_packages`
   - `agents.<agent_name>`
3. `plan`
   - `max_iterations`
   - `global.skill_packages`
   - `agents.<agent_name>.mode`

`plan.agents.<name>.mode` 可选值：

1. `readonly_only`
2. `readonly_plus`
3. `all`

配置持久化目录：

1. 默认模板：`carto_agent/defaults/agent_configs/default/agent.yaml`
2. 运行目录：`AUTOCARTO_HOME/config/agent_configs/<config_name>/agent.yaml`
3. 当前激活：`AUTOCARTO_HOME/config/agent_configs/.active_agent_config`

重要约束：

1. `runtime` 不能为空，且 `runtime.agents` 至少一个。
2. `default` 配置不可删除。
3. 主 agent 不可被删除。

---

## 7. 工具/技能目录管理与优先级

关键环境变量：

1. `AUTOCARTO_HOME`：运行时 home（Go 侧强依赖，含 `config/` 与 `data/`）
2. `AUTOCARTO_ASSETS_ROOT`：资产根目录（含 `skills/system` 标记）

工具目录（运行时组合）：

1. 插件资产 tools（system/user）
2. `AUTOCARTO_HOME/config/tools/global`
3. `AUTOCARTO_HOME/config/agent_configs/<active>/tools`

技能目录（运行时组合）：

1. 插件资产 skills（system/user）
2. `AUTOCARTO_HOME/config/skills/global`
3. `AUTOCARTO_HOME/config/agent_configs/<active>/skills`

Scope 语义（skill service）：

1. `builtin`
2. `user`
3. `global`
4. `exclusive`
5. `all`

---

## 9. REST 接口清单（治理常用）

运行时与目录：

1. `GET /api/runtime/snapshot`
2. `GET /api/runtime/services`
3. `GET /api/runtime/descriptor`
4. `GET /api/tools/catalog?scope=global|active|effective`
5. `GET /api/services/ports`

智能体配置：

1. `GET /api/agent-configs`
2. `POST /api/agent-configs`
3. `GET /api/agent-configs/{name}`
4. `GET /api/agent-configs/{name}/data`
5. `PUT /api/agent-configs/{name}`
6. `DELETE /api/agent-configs/{name}`
7. `POST /api/agent-configs/{name}/activate`
8. `POST /api/agent-configs/{name}/clone`

技能与包：

1. `GET /api/skill-packages`
2. `GET /api/skill-packages/{name}`
3. `POST /api/skill-packages/{name}/toggle`
4. `POST /api/skill-packages/migrate`
5. `GET /api/skills`
6. `PUT /api/skills/{name}`
7. `DELETE /api/skills/{name}`
8. `POST /api/skills/import`

---

## 9. 标准操作清单（重点执行版）

### 9.1 新增工具（从定义到可调用）

输入准备：

1. 工具唯一名（建议 `<domain>_<action>`）。
2. 执行方式（`handler` / `handler_file+handler_function` / `backend_tool`）。
3. 参数清单（名称、类型、是否必填、默认值）。

执行步骤：

1. 编写工具定义（`tool/v1` 或 `tool_contract/v1`），最少包含 `metadata/spec/execution/policy`。
2. 若选择 `handler`，同步实现 Python handler；若选择 `backend_tool`，确认后端能力已存在。
3. 调用 `validate_skill_definition` 做结构校验。
4. 如无目标包，先 `create_skill_package`；再用 `save_skill_to_package` 绑定工具定义。
5. 有脚本依赖时，用 `save_skill_script` 写入包内脚本文件。
6. 通过 `list_skill_packages` 确认包已可见，`tool_refs` 已包含目标工具。
7. 发布可见性控制：
   - 对外发布：`policy.visibility=public`
   - 内部灰度：`hidden/internal`

#### 9.1.1 工具注册时到底要改哪些文件

以下按“最常见的 QGIS handler 工具”给出必改文件：

| 文件 | 必改内容 | 备注 |
|---|---|---|
| `autocarto/tools/system/<domain>/<tool_name>.yaml` | 定义 `metadata/spec/execution/policy`；至少包含 `metadata.name`、`metadata.runtime`、`spec.parameters`、`execution.handler`、`policy.visibility` | **工具注册主文件**，不改就不会进入 authority 定义 |
| `autocarto/tools/system/<domain>/handlers/<module>.py` | 新增或导出 handler 函数 | `execution.handler` 指向这里的模块函数路径 |
| `autocarto/tools/system/<domain>/handlers/__init__.py` | 将新增 handler 加入导出列表（`__all__`） | 保持 domain handler 入口一致 |
| `autocarto/tools/system/_shared/<xxx>_impl.py` | 写具体业务实现（参数校验/调用 QGIS API/返回结构） | 建议把核心逻辑放 `_shared`，domain 侧只做薄封装 |
| `autocarto/skills/system/<package>/SKILL.md` | frontmatter 增加工具引用（`tool_refs` 或 `tools`） | 让技能包显式声明依赖工具，便于后续分配给 agent |

如果走 Creator Tools（推荐日常治理）会自动落盘到 runtime 目录，重点看这两个文件是否生成：

1. `AUTOCARTO_HOME/config/skills/global/<package_id>/SKILL.md`
2. `AUTOCARTO_HOME/config/tools/global/<package_id>/<tool_name>.yaml`

如果你新增的是 **creator-tools 自身**（即 `execution.backend_tool=<new_tool>`），还要改后端：

1. `carto_agent/internal/creatortools/tools.go`
   - 新增工具常量
   - 在 `handlerRegistry` 注册 handler
   - 实现对应处理函数
2. `autocarto/tools/system/creator-tools/<new_tool>.yaml`
   - 声明 `execution.backend_tool: <new_tool>`
3. `autocarto/skills/system/creator-tools/SKILL.md`
   - `tools` 列表加入 `<new_tool>`

#### 9.1.2 完整示例：新增 `canvas_pan_to_center`

目标：新增一个把画布中心移动到指定坐标的工具（QGIS handler 模式）。

步骤 A：新增工具定义文件  
文件：`autocarto/tools/system/canvas-navigation/canvas_pan_to_center.yaml`

```yaml
apiVersion: tool/v1
kind: Tool
metadata:
  name: canvas_pan_to_center
  display_name: canvas_pan_to_center
  domain: canvas-navigation
  runtime: qgis
spec:
  description: 将地图画布中心移动到指定坐标
  parameters:
    - name: x
      type: number
      required: true
      description: 目标中心点 X 坐标
    - name: y
      type: number
      required: true
      description: 目标中心点 Y 坐标
execution:
  handler: handlers.canvas.pan_to_center_handler
  param_mapping:
    x: "{{ x }}"
    y: "{{ y }}"
policy:
  visibility: public
```

步骤 B：实现 handler 逻辑  
文件：`autocarto/tools/system/_shared/canvas_navigation_impl.py`  
新增函数（示意）：

```python
def pan_to_center_handler(params: Dict[str, Any]) -> Dict[str, Any]:
    try:
        iface, _, _, _ = _runtime_dependencies()
        x = params.get("x")
        y = params.get("y")
        if x is None or y is None:
            return {"success": False, "output": "", "info": "错误：缺少 x/y 参数"}
        if not iface:
            return {"success": False, "output": "", "info": "无法访问QGIS界面"}

        from qgis.core import QgsPointXY
        canvas = iface.mapCanvas()
        canvas.setCenter(QgsPointXY(float(x), float(y)))
        _safe_refresh_canvas()
        return {"success": True, "output": f"{x},{y}", "info": "画布中心已更新"}
    except Exception as e:
        return {"success": False, "output": "", "info": f"移动画布中心失败: {str(e)}"}
```

步骤 C：在 domain handler 入口导出  
文件：`autocarto/tools/system/canvas-navigation/handlers/canvas.py`

1. `from autocarto.tools.system._shared.canvas_navigation_impl import ... pan_to_center_handler`
2. `__all__` 增加 `"pan_to_center_handler"`

步骤 D：声明技能包引用  
文件：`autocarto/skills/system/canvas-navigation/SKILL.md`  
在 frontmatter 的 `tools:`（或 `tool_refs:`）中加入：

```yaml
- canvas_pan_to_center
```

步骤 E：验收（必须三项都通过）

1. `GET /api/runtime/descriptor` 中 `execution_manifest` 有 `canvas_pan_to_center`。
2. `GET /api/tools/catalog?scope=active` 能看到该工具（若 `visibility=public`）。
3. 发起一次调用并收到 `tool_result.terminal_state=succeeded`。

完成标准（必须全部满足）：

1. `GET /api/runtime/descriptor` 的 `execution_manifest` 出现该工具。
2. `GET /api/tools/catalog?scope=active` 中可见性与预期一致。
3. 真实触发一次 `tool_invoke`，收到 `tool_result` 且 `terminal_state` 为终态。

### 9.2 注册智能体（从配置到激活）

输入准备：

1. 新配置名（如 `spatial_ops_v1`）。
2. 目标 skill 包列表（按 runtime/plan 分组）。
3. 需要挂载的专用 agent 名称（如 `spatial_analyst`）。

执行步骤：

1. 调用 `create_agent_config`，建议 `clone_from=default`。
2. 调用 `assign_skill_packages` 分层绑定：
   - `mode=runtime,target=global`：全局运行技能
   - `mode=runtime,target=agent`：指定 agent 专属技能
   - `mode=plan,target=global|agent`：规划阶段能力（按需）
3. 仅在需要整体覆盖时再用 `update_agent_config`。
4. 激活配置：`POST /api/agent-configs/{name}/activate`。

完成标准（必须全部满足）：

1. `GET /api/runtime/snapshot` 中 `runtime_active_agent_config` 为新配置名。
2. `GET /api/agent-configs/{name}/data` 中 skill 分配结构与预期一致。
3. `GET /api/tools/catalog?scope=active` 反映出该配置下的有效工具集。

### 9.3 联调与回归（工具 + 智能体）

1. 联调顺序固定：`descriptor -> catalog -> ws invoke/result`。
2. 记录三类版本号：`runtime descriptor revision`、`tool catalog revision`、`active agent config`。
3. 每次变更至少保留一个可复现调用样例（入参 + 终态结果）。

---