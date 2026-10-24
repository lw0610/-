# AutoCarto 修复优化方案 v2：资源体系与运行链路

> 执行状态：本轮推荐范围（全部 R 任务及 G2/G4/G5）已完成，完整门禁 exit 0，五项真实 QGIS 离线验收通过。见 [执行状态](evidence/00-执行状态.md) 和 [R73 整体验收与边界](evidence/R73.md)。G1/G3 按建议另行处理。
>
> 原方案编写日期：2026-10-22。编写时依据：`enio2` 工作区当前代码（基线提交 `77710c4` 之上尚未提交的重构，共改 560、删 171、新增 195 个文件）。上一版方案见 [`../refactor/`](../refactor/README.md)，本方案不推翻它的概念模型（工具包、流程包、资源），只修复其落地后暴露的问题，重点是**公共资源**。
>
> 本目录可以直接交给执行智能体。先读本文件，再读被分配任务卡（`07-任务卡.md`）里"必读"列出的规范。

## 0. 文档清单与阅读顺序

| 文件 | 内容 | 谁必须读 |
|---|---|---|
| `README.md`（本文件） | 目标、问题与证据、目标模型、阶段与依赖、全局约束、验收、待确认事项 | 所有人 |
| `01-资源模型规范.md` | 资源清单 `resource.yaml`、编号、三层归属、名称解析、覆盖、版本与摘要、`flow.lock`、导出导入、绑定、新增诊断码 | 后端、前端、内容 |
| `02-解析与交付规范.md` | 唯一解析器接口、资源句柄 JSON、工具参数 `type: resource`、流程作用域、默认资源与资源契约、步骤消息、Python 消费方式 | 后端、工具层 |
| `03-资源格式规范.md` | 格式规范单一来源文件、各类资源的列/别名/取值、XLSX 规则、数据文件夹扫描、shapefile 附属文件、示例校验 | 后端、内容 |
| `04-编译与链接规范.md` | 编译（调模型）与链接（解析资源）拆分、缓存键与指纹、可用流程与方案环境缓存 | 后端 |
| `05-迁移清单.md` | 现有资源、代码点、绑定、兼容层逐项去向 | 所有执行智能体 |
| `06-整体治理.md` | 资源之外的整体设计问题（场景代码残留、FLOW.md 补丁、分层、工具目录、残留文件）的方案 | 后端、内容 |
| `07-任务卡.md` | 全部任务卡、依赖、文件归属表、泳道 | 所有执行智能体 |
| `evidence/` | 每张卡完成后的验证记录 `Rxx.md` / `Gx.md` | 所有执行智能体 |

## 1. 目标

**一句话**：资源从现在的"目录 + 体检器"，变成"有身份、可移植、由唯一解析器解析、以结构化句柄交给工具"的一等对象。做到：体检通过即运行可用；用户覆盖对所有代码路径一致生效；分享流程不丢依赖。

具体四件事：

1. **一种身份、一套规则**：资源有稳定编号、类型、版本和摘要；类型判定只有一套；FLOW.md 只有一种推荐引用写法。
2. **唯一解析链**：只有 Go `internal/resource` 解析资源。工具拿到的是解析后的"资源句柄"，不再自己找文件、读对照表、写死内置路径；模型只需要写资源名称。
3. **按归属分三层**：工具资产（工具私有）、流程自带（随流程导出）、公共资源（跨流程复用）。场景专用内容不进公共资源库。
4. **编译与链接分离**：调模型的编译只看流程声明；资源解析、文件存在性、绑定每次运行前确定性计算。改数据、换绑定不再触发模型编译，每轮对话不再全量哈希。

## 2. 现状问题（证据均来自当前代码）

标"已复现"的条目已用 `go test -overlay` 挂临时测试实测（未改动仓库）；标"已确认"的条目来自代码阅读。

**P1 体检与执行走两条解析链（最严重）**
Go `internal/resource` 只用于体检和拼步骤消息；工具执行时 Python 自己找资源，多处写死内置路径，绕过用户覆盖和绑定：
- `autocarto/tools/system/_shared/style_set_support.py:21` 固定读 `国标底图样式/presets.json`；
- `autocarto/tools/system/_shared/layout_compose_impl.py:112` 自己实现"用户→内置"查找且只认 `.layout.yaml`；
- `autocarto/tools/system/_shared/admin_boundary_impl.py:46` 固定读 `resources/system/data/中国行政区边界`；
- `autocarto/tools/system/_shared/print_layout_advanced_impl.py:529` 固定读 `symbols/图例/路线箭头.jpg`（对照表登记的是 `.png`，目录里两份并存）；
- `autocarto/knowledge/visual_presets.py:13` 以及 `route_drawing_impl.py`、`route_points_impl.py`、`symbolize_mapping_impl.py`、`callouts_impl.py`、`autocarto/core/config_manager.py` 直接读 `styles/route-defaults.json`。

**P2 资源表解析器至少四份，已经分叉**
Go `internal/resource/table.go`；Python `table_io.py`、`style_set_support.py`、`knowledge/visual_presets.py`。对照表文件名候选又在 `resolve.go`、`flowrun/executor_route_knowledge.go`、`visual_presets.py` 各写一遍。分叉表现见缺陷 D4、D5。

**P3 资源交付靠模型抄路径**
`internal/flowrun/step_message.go:18` 把整个流程的全部资源及每个条目绝对路径写进每一步，不按本步引用过滤，却不写数据清单的基准目录（D7）。只有专用执行器（`executor_route_knowledge.go:62`）按引用结构化传参——正确模式已存在但没有推广。

**P4 资源身份只有"类型 + 文件名"**
无稳定编号、版本、摘要；流程用 `类型「名称」：公共资源库` 按名引用；`internal/flow/export.go:13` 只打包流程目录，分享出去的流程要么找不到资源，要么静默用上对方同名但内容不同的资源。

**P5 静默覆盖且不可见**
同名用户资源静默覆盖内置资源（`internal/resource/library.go:171` 用户优先）；前端把被覆盖的内置项隐藏（`autocarto-web/src/features/flows/ResourceLibrary.tsx:27`）；没有"谁在用这个资源"。

**P6 类型判定两套、引用写法五种**
公共库按"分组目录 + 扩展名"判定（`library.go:71`），流程内按 FLOW.md 类型词 + `.qml` 后缀判定（`internal/flow/parse.go:440`）。位置写法有"公共资源库"、裸版式名、流程内相对路径、绝对路径、项目根相对路径五种。`styles/` 下任意 CSV 都会被列为路线样式。

**P7 资源归属混乱**
公共资源库里混放：工具私有数据/配置（中国行政区边界，含 `tests/*.py`、`templates/*.py`、`upstream/SKILL.md`；`route-defaults.json`；`presets.json`）、单一场景专用内容（军标、路线样式-红蓝对比、图例/路线箭头）和真正可复用的资源。`route-defaults.json` 的 legacy 段仍有"国军路线名称""会师军队名称"。场景词守卫把 `autocarto/resources`、`autocarto/knowledge` 整目录排除（`autocarto/tests/test_scene_word_guard.py:31`），所以测不出来。

**P8 资源内容被卷进模型编译缓存**
`internal/flow/cache.go:20` 的编译指纹全文哈希每个资源及数据条目文件；配置了编译模型时，改一个 shp 或换一次绑定就整份重新调模型编译。每次规划/执行都会 `injectAvailableFlows → AvailablePlans → EnsureCompiled`，每个启用流程两次 `source`，各自 `LoadEnvironment` + 全量哈希（`internal/flow/compile.go:108`、`:136`）。前端绑定请求因此需要 300 秒超时（`autocarto-web/src/features/flows/api.ts` 的 `bind`）。

**P9 绑定不稳定**
绑定键是 `resource_` + sha256(类型+标签) 前 12 位（`internal/flow/compile.go:93`），改标签即丢绑定；绑定对话框让用户手填这个"资源编号"（`FlowWorkspace.tsx:298`）；绑定文件路径三处各自推导（`config.ResolveResourceBindingsFile`、`internal/flow/bindings.go:17`、验收程序）；现有绑定值 `../数据/底图数据/China_map` 是越出项目根的相对路径。

**P10 管理操作缺失**
没有替换（同名导入直接冲突，只能先删，期间流程静默回落内置）、没有复制内置为用户资源（只读错误文案却让用户"先复制"）、没有引用查询与删除保护。

**P11 文档与代码不一致**：《资源制作指南》符号表示例表头 `SVG路径` 实际不被接受（D5）。

**P12 未登记的兼容层**：`presets.json` 旧预设索引、`route-defaults.json` legacy 段、`legend_arrow.jpg` 回退、`include_military_arrow` 参数、`defaults/tool_contracts` 遗留契约，均未登记到 `carto_agent/defaults/architecture/removal_ledger.yaml`，违反上一版 §7.3。

### 已复现 / 已确认的缺陷（阶段 0 修复）

| 编号 | 缺陷 | 位置 | 状态 |
|---|---|---|---|
| D1 | 资源库"引用到当前流程"对单个样式写出 `样式「X」：公共资源库`，被解析为样式集 → 找不到 | `internal/flow/parse.go:440`、`autocarto-web/src/features/flows/editor.ts:102` | 已复现 |
| D2 | 数据文件夹把 `meta.json`、`upstream-manifest.json` 当成矢量图层 | `internal/resource/resolve.go:219` | 已复现 |
| D3 | 用户放同名 `四角对齐版式.qpt`：体检解析到用户 .qpt，`layout_compose` 仍用内置 `.layout.yaml` | `library.go:171` 与 `layout_compose_impl.py:112` | Go 侧已复现，Python 侧已确认 |
| D4 | 样式集：Go 接受 `对照表.xlsx`/`mapping.csv`，Python 只认 `对照表.csv`；XLSX：Go 遍历工作表，Python 只读第一张 | `resolve.go:268`、`style_set_support.py:55`、`table_io.py:48` | 已确认 |
| D5 | 指南示例表头 `SVG路径` 报"对照表缺少必填列 svg_path" | `internal/resource/table.go`、`docs/资源制作指南.md:72` | 已复现 |
| D6 | `route_drawing_impl.py` 两处无参调用 `load_route_styles()`，自定义路线样式表的 `reference_style_id`/`auto_width_group` 被内置红蓝表顶替 | `route_drawing_impl.py:408`、`:480` | 已确认 |
| D7 | 步骤消息不写数据清单基准目录；`data_load_list` 缺省用清单所在目录 → 绑定在工具侧失效 | `step_message.go:19`、`data_loading_impl.py:260` | 已确认 |
| D8 | shapefile 只检查 `.shp`：缺 `.shx`/`.dbf` 不报错，改 `.dbf` 不使缓存失效 | `resolve.go:187`、`cache.go:32` | 已确认 |

资源之外的整体设计问题（场景代码仍在核心、补丁迁入 FLOW.md、`websocket` 包过大、工具三套分类、残留文件、重构未提交）见 `06-整体治理.md`。

**根因**：资源被设计成"给体检看的目录"，而不是"运行时的依赖"。没有资源身份，就无法锁定、导出、覆盖和反查；没有唯一解析入口，每个消费者就各自实现一遍查找和解析。

## 3. 目标模型

### 3.1 资源的三层归属

| 层 | 放在哪 | 谁能看到 / 修改 | 怎么被引用 | 例子 |
|---|---|---|---|---|
| 工具资产 | `autocarto/tools/system/<工具域>/assets/` | 开发者；界面不出现、不进资源库 | 工具代码按自身目录读取，或工具 YAML 声明 | 中国行政区边界数据、路线渲染默认参数、旧预设索引 |
| 流程自带 | `<流程目录>/resources/<分组>/` | 流程作者；随流程导出 | FLOW.md：`类型「名称」：resources/...` | 军标、路线样式-红蓝对比、路线箭头、中国底图清单 |
| 公共资源 | 内置 `autocarto/resources/system/<分组>/`；用户 `$AUTOCARTO_HOME/resources/<分组>/` | 所有流程；内置只读 | FLOW.md：`类型「名称」：公共资源库`（编译后在 `flow.lock` 锁定编号） | 国标底图样式、四角对齐版式、统计专题版式、路线样式-单色 |

### 3.2 一个资源 = 主文件（或文件夹）+ 资源清单

- 文件夹型资源（样式集、符号集、数据文件夹）：文件夹内放 `resource.yaml`。
- 单文件资源（数据清单、样式、路线样式、版式、文档）：主文件旁放同名 `<资源名>.resource.yaml`。
- 清单给出 `id`（稳定编号）、`type`、`name`（显示名）、`version`、`entry`、可选 `overrides`。没有清单的旧式资源按现规则推断并提示升级。详见 `01`。

### 3.3 运行链路

```text
FLOW.md「使用的资源」声明（类型词「名称」：位置）
        │ 解析（确定性）
        ▼
编译（调模型，只看声明身份）──► 编译计划（缓存，键不含资源内容）
        │
        ▼ 链接（确定性，每次预览/运行前；唯一入口 internal/resource.Resolver）
链接计划 = 编译计划 + 资源快照（编号、版本、摘要、规范化条目）+ 资源诊断 + flow.lock
        │
        ├──► 步骤消息：只列本步资源的名称和用途，不铺路径
        │
        └──► 工具调用：参数声明为 type: resource
                 └─ 执行引擎（internal/skill）用 Resolver 把"名称/路径"展开为资源句柄 JSON
                        └─ Python 工具只读句柄里的 entries，不读对照表、不找文件
```

## 4. 阶段与任务

详细任务卡、文件归属表见 `07-任务卡.md`。

| 阶段 | 任务 | 做完后 |
|---|---|---|
| 0 检查点与止血 | R00 提交检查点；R01 Go/前端缺陷修复；R02 Python 缺陷修复 | D1–D8 修复，现有架构下行为一致 |
| 1 资源核心 | R10 格式规范单一来源；R11 资源清单、编号与名称解析；R12 工具资产迁出 | 资源有身份；列定义只有一份；工具私有数据离开资源库 |
| 2 句柄交付 | R20 工具资源参数与句柄展开；R21 流程作用域、步骤消息、`resource_get`；R22 数据与样式工具改用句柄；R23 版式、路线、图例、知识工具改用句柄 | 工具只消费句柄，Python 不再解析资源表 |
| 3 归属重划 | R30 场景资源迁入流程包与执行器资源契约；R31 绑定迁移与单一绑定存储 | 公共库只剩可复用资源；绑定按编号保存 |
| 4 编译与链接 | R40 编译/链接分离；R41 方案环境与可用流程缓存 | 改数据/绑定不调模型；每轮对话不全量重算 |
| 5 可移植与管理 | R50 `flow.lock` 与导出导入打包；R51 资源库管理接口 | 分享不丢依赖；可复制、替换、反查、删除保护 |
| 6 前端 | R60 资源库界面；R61 流程资源页、绑定与诊断修复 | 覆盖关系可见；绑定不再填编号 |
| 7 收尾 | R70 守卫扩展与兼容层登记；R71 体检—执行一致性契约测试；R72 文档；R73 端到端验收 | 旁路与场景词被测试拦住；验收完成 |
| 治理（可并行） | G1 路线知识场景隔离（需确认）；G2 内置 FLOW.md 去补丁；G3 `websocket` 分层（需确认）；G4 工具目录与遗留契约；G5 残留清理 | 见 `06` |

主要依赖（每项写"任务 ← 前置任务"；完整依赖与文件归属见 `07` §1、§3）：

```text
R01 ← R00          R02 ← R00          R10 ← R01          R12 ← R02
R11 ← R10, R12     R20 ← R11          R21 ← R20          R22 ← R20, R12
R23 ← R22          R31 ← R11, R21     R40 ← R31          R41 ← R40
R30 ← R23, R40     R50 ← R30, R41     R51 ← R11          R60 ← R51
R61 ← R60, R50     R70 ← R30          R71 ← R30          R72 ← R61, R71
R73 ← 全部 R 任务

关键路径：R00 → R01 → R10 → R11 → R20 → R21 → R31 → R40 → R30 → R50 → R61 → R72 → R73
```

建议泳道（每条泳道交给一个执行智能体，按顺序做；跨泳道依赖时等待）：

| 泳道 | 任务顺序 | 技能要求 |
|---|---|---|
| A Go 资源核心 | R01 → R10 → R11 → R51 | Go |
| B Go 运行链路 | R20 → R21 → R31 → R40 → R41 → R50 | Go、Eino ADK |
| C Python 工具 | R02 → R12 → R22 → R23 | Python、PyQGIS |
| D 前端 | R60 → R61（可先按接口约定用模拟数据开发） | React、Next.js |
| E 内容与收尾 | R00 → R30 → R70 → R71 → R72 → R73 | 熟悉 GIS 制图与本仓库测试 |
| F 治理（可选） | G5 → G2 → G4 →（确认后）G1 → G3 | 视任务 |

阶段表中的阶段是概念分组；实际开工顺序以依赖和泳道为准（例如 R31 在 B 泳道、R30 要等 R40）。

## 5. 整体验收标准

全部满足才算完成：

1. **V1 一致**：同一资源在体检和执行中使用同一解析结果。契约测试（R71）覆盖 8 类资源与 D1–D8 用例全部通过。
2. **V2 无旁路**：生产 Python 代码不再出现 `resources/system` 路径、不再解析资源对照表；Go 中只有 `internal/resource` 解析资源（R70 守卫测试）。
3. **V3 可移植**：导出（附带依赖）的流程包在空白运行目录导入后，除需绑定的外部数据外体检零资源错误；改 FLOW.md 中资源标签后绑定仍生效。
4. **V4 显式覆盖**：同名资源不再静默覆盖；覆盖关系在界面可见；删除或替换前显示受影响的流程。
5. **V5 性能**：修改数据文件或绑定不触发模型编译；连续两次对话之间不重复加载方案环境、不做全量内容哈希（计数测试）。
6. **V6 去场景化**：扩展后的场景词守卫（含公共资源库与英文标识）零新增违规；场景专用资源只在流程包内。
7. **V7 不退化**：黄金样例回归与上一版 T53 五项离线验收（行军质量审查开/关、统计图、人口密度、考察路线）保持通过。
8. **V8 门禁**：`./build.sh check` 通过。

## 6. 全局执行约束（所有执行智能体必须遵守）

1. **一次只做一张任务卡**。只改卡中"涉及范围"的文件；确需改范围外文件，先在验证记录说明原因。同一文件同一时间只允许一张卡修改（见 `07` 文件归属表）。
2. **不破坏用户数据**：不得修改或删除 `autocarto_data/` 下的用户数据和配置。只有 R31 可以迁移 `config/resource_bindings.yaml`，且必须先备份、迁移可重复执行（幂等）。
3. **资源只由 Go `internal/resource` 解析**：新代码不得在 Python 中解析资源对照表（CSV/XLSX 资源表），不得在任何代码中写 `resources/system` 下的具体资源路径或资源名常量。工具需要默认资源时，用工具 YAML 的 `default_resource: <资源编号>`。
4. **工具私有数据放工具目录 `assets/`**，不进公共资源库；**场景专用内容只能放在流程包内**（含其 `resources/`）。
5. **兼容层要登记**：保留旧接口、旧格式时，在 `carto_agent/defaults/architecture/removal_ledger.yaml` 按现有格式追加条目（写明删除条件与阻塞测试），并保证 `go test ./internal/control -run TestCompatLayerRequiresRemovalCriteria` 通过。多张卡并行时只追加条目，不改他人条目。
6. **测试测行为，不测措辞**：不得断言提示词、SKILL.md、FLOW.md 的具体文字；可以断言结构、诊断码、解析结果、句柄内容和工具行为。
7. **用户可见文案用中文**；代码标识符、文件名中的固定字段、资源编号用英文。
8. **每张卡完成后**，在 `docs/refactor-v2/evidence/<任务号>.md` 写验证记录（格式见 `evidence/README.md`）。
9. **以代码事实为准**：本方案与代码现状不一致时，以代码为准实现目标，并在验证记录写明差异。
10. **质量门禁**：提交前至少运行卡中列出的测试；涉及多个子系统时运行 `./build.sh check`。真实 QGIS 测试用能导入 QGIS 的解释器（`build.sh` 中的 `PYTHON_BIN`，macOS 通常为 `/Applications/QGIS-LTR.app/Contents/MacOS/python` 或 `/Applications/QGIS.app/Contents/MacOS/python`），`test_*_qgis.py` 每个模块单独进程运行（参考 `scripts/refactor/run_python_tests.py`）。
11. **提交**：每张卡至少一个提交，提交信息以任务号开头（例如 `R11: 资源清单与编号`）。不推送、不合并到 `main`，由用户决定。

## 7. 交给执行智能体时的提示词

把下面这段连同任务号发给执行智能体：

```text
你是本仓库的执行智能体，请完成任务 Rxx（或 Gx）。
1. 先阅读 docs/refactor-v2/README.md（特别是 §6 全局执行约束），再阅读 docs/refactor-v2/07-任务卡.md 中该任务的任务卡，以及卡中"必读"列出的文档。
2. 只修改任务卡"涉及范围"中的文件；确需修改范围外文件时，在验证记录中说明原因。开工前确认 07 文件归属表中，你要改的文件没有被正在进行的其他任务占用。
3. 完成后运行任务卡中的测试命令，按 docs/refactor-v2/evidence/README.md 的格式写 docs/refactor-v2/evidence/<任务号>.md，并提交（提交信息以任务号开头，不推送）。
4. 本方案与代码现状不一致时，以代码为准实现任务目标，并在验证记录中写明差异。
5. 不要开始其他任务。
```

## 8. 待确认事项（开工前由用户确认）

| # | 事项 | 建议 | 影响的任务 |
|---|---|---|---|
| 1 | 国标底图样式对照表中的"河南省地级城市驻地""河南铁路""河南高速公路"等别名 | 删除。内置流程的图层名来自数据清单，不依赖这些别名；旧工程如需要，可复制样式集为用户资源后自行添加 | R30 |
| 2 | 专用执行器"路线知识抽取"的默认符号集与路线样式 | 取消默认，改为"资源契约"：使用该执行器的流程必须声明符号集和路线样式，缺失时体检报错并给修复按钮。备选：新建中性的"通用路线点"符号集作为默认 | R30 |
| 3 | 三个旧预设工具 `layer_set_marker_symbol` / `layer_set_line_symbol` / `layer_set_fill_symbol` | 从工具包隐藏并登记下线，`presets.json` 随之迁为工具资产。黄金样例的旧基线（`run_golden.py` 非 compose 模式）仍直接调用它们，先把这条基线改为样式集路径，再在下一个版本删除。备选：保留为隐藏工具不删除 | R12、R70 |
| 4 | 同名资源冲突策略 | 覆盖必须显式（`overrides`）；同名且未声明覆盖时按名称引用报"歧义"并让用户选择。备选：保留"用户优先"，但界面显示覆盖关系 | R11、R60 |
| 5 | 导出流程包时是否默认附带依赖的公共资源 | 默认附带（可取消勾选） | R50、R61 |
| 6 | G1（路线知识子系统场景隔离）、G3（`websocket` 分层）是否纳入本轮 | 本轮只做 G2、G4、G5；G1、G3 单独立项 | 06、07 |
| 7 | 根目录与 `autocarto/` 下两份内容不同的 `symbology-style.db`（代码中未发现引用） | 确认用途后保留一份或都移出仓库 | G5 |

## 9. 术语

| 术语 | 含义 |
|---|---|
| 资源 | 数据清单、数据文件夹、样式、样式集、路线样式、版式、符号集、参考文档之一；由主文件（或文件夹）和资源清单组成 |
| 资源清单 | `resource.yaml`（文件夹型）或 `<资源名>.resource.yaml`（单文件型），记录编号、类型、名称、版本、主文件、覆盖关系 |
| 资源编号 | 清单中的 `id`，如 `layout.four-corner`；稳定不变，绑定、锁定、覆盖、默认资源都用它 |
| 归属 | 工具资产 / 流程自带 / 公共资源（内置、用户） |
| 解析器 | Go `internal/resource.Resolver`，系统中唯一解析资源的入口 |
| 资源句柄 | 解析器输出、交给工具和执行器的 JSON（`resource-handle/v1`），含规范化条目 |
| 规范化条目 | 句柄中的 `entries`：数据清单的文件、样式集的图层—样式映射、符号集的符号等，字段固定 |
| 编译 | 调模型推断输入、产出、建议能力；结果按声明身份缓存 |
| 链接 | 确定性地解析资源、检查存在性、套用绑定和锁定；每次预览/运行前进行 |
| `flow.lock` | 流程目录中的锁定文件，记录公共资源引用对应的编号、版本和摘要 |
| 资源契约 | 使用方（专用执行器）对资源的要求，例如符号集必须包含 `waypoint` |
| 绑定 | 数据清单相对路径的基准目录，保存在 `config/resource_bindings.yaml` |
