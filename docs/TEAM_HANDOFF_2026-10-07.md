# GEE、浏览器与在线取数移植交接（2026-10-07）

本文交接 Eino / AutoCarto 当前源码中的在线取数能力，面向维护 Windows `AutoCarto_go + MapGIS + Web` 的队友。它是移植说明，不是 Windows 发布验收报告，也不表示整个 GeoCode 已移植。

本仓库的 GIS 执行端是 **QGIS / PyQGIS**，后端是 Go / Eino，前端是 React / Next.js / Tauri。Windows MapGIS 的 C# 插件、工具注册、图层对象、标注和布局接口应保留其自身实现；不能把这里的 QGIS Python handler 当作 MapGIS handler 使用。GEE、浏览器和公开网页下载主要运行在 Go 后端，可以先独立接入，再适配结果到 MapGIS 的加载环节。

## 1. 交接范围与参考来源

本轮主要内容：

- 独立 Python GEE 执行、区域读取、覆盖检查、单日期候选筛选、分块下载、进度、取消及会话内完成文件复用。
- 完整中国省、市、县边界包及按唯一身份选取的模板，补齐空工程没有研究区文件时的数据准备路径。
- 每会话独立的 Playwright MCP 浏览器，以及公开网页读取和文件下载。
- 在线数据设置、显式环境诊断、工具契约、专家能力分配、相关中断恢复和会话清理适配。

GEE 流程参考 `zzhonglei/GeoCode-Release`：执行帮助函数依据源码发行版 v0.9.6，技能和边界资产固定到提交 `6e3534f7af3675b19bd3c5d54c94022959dccdd2`。这份实现做了本地接口与可靠性适配，并非原应用整体复制。迁移时一起保留以下归因、许可、完整资产及清单：

- [GEE 来源与适配](../autocarto/skills/system/gee-scripting/UPSTREAM.md)、同目录 `LICENSE`、`references/`、`templates/`。
- [行政区来源与适配](../autocarto/skills/system/china-admin-boundaries/UPSTREAM.md)、同目录 `LICENSE`、`upstream-manifest.json`、`upstream/`、`data/`、`templates/`。
- [功能配置与验证记录](online-data-and-gee.md)、[GEE 运行时接口](../carto_agent/internal/geeruntime/README.md)。

行政区资产是上游完整的34省、375市、2891县，共3300要素，不是仅保留某个测试城市。来源声明、CRS 和原始哈希见清单；数据存在不等于已独立认证全部坐标精度或行政区时效。

## 2. 按模块迁移，不只复制 Python 下载器

| 本仓库位置 | 职责 | Windows / MapGIS 侧注意事项 |
| --- | --- | --- |
| `carto_agent/internal/config/integrations.go` | GEE / browser / web 配置、默认值、校验 | 合并到 Windows 配置服务，保留服务分别开关、路径和超时字段。不要覆盖用户整个配置。 |
| `carto_agent/internal/integrations/` | 内部工具分发、诊断、会话输出目录 | 接到 Windows 后端工具路由；会话标识与取消上下文必须传递。 |
| `carto_agent/internal/geeruntime/` | Python worker、下载、日志、进度、任务记录、进程管理 | 整包迁移并核对依赖；保留 Windows 进程树终止代码。读取返回结果时，即使同时有错误也应保留日志、完成文件和已提交任务 ID。 |
| `carto_agent/internal/browserruntime/` | MCP 会话、工具发现与调用、输出、清理 | 按聊天隔离进程；保留 Windows `npx.cmd` 解析和取消逻辑，不能直接照搬 macOS 可执行文件路径。 |
| `carto_agent/internal/webdata/` | 公开网页读取、文件下载、大小限制及来源信息 | 这是普通 HTTP 工具，不共享浏览器登录状态，不执行网页 JavaScript。 |
| `carto_agent/internal/api/`、`internal/websocket/` | 配置检查接口、进度转发、会话生命周期 | 核对 `/api/integrations/check`、GEE 进度交付、停止及删除会话的连接点。不能只迁移工具调用，遗漏取消和清理。 |
| `carto_agent/cmd/agent/main_deep.go`、`internal/deepagent/`、`internal/hitl/` | 内部工具接入、按 agent 配置过滤、工作流程读取与人工确认 | Windows 若结构不同，按现有 registry / descriptor 接口适配；不要把专家工具无条件暴露给主智能体。 |
| `autocarto/tools/system/gee-remote-sensing/`、`browser-data-acquisition/` | 工具名称、参数 schema、执行绑定 | 与 Go 内部执行入口一并注册；这些工具不是交给 MapGIS C# 插件执行的 GIS 操作。 |
| `autocarto/skills/system/gee-remote-sensing/`、`gee-scripting/`、`china-admin-boundaries/`、`browser-data-acquisition/` | 工具分类、可读流程与本地附件 | 必须随安装包部署；保持相对目录和 frontmatter 分类，不能只复制 SKILL.md。 |
| `autocarto-web/src/components/settings/IntegrationsConfigWorkspace.tsx`、`src/lib/api.ts` | 设置表单、保存、环境检查与错误显示 | 合并 Windows Web 对应页面、类型和 API envelope；检查旧后台不支持接口时的提示。 |
| `autocarto/` 中 GIS 数据核验、加载与布局工具 | 将成果接续到 QGIS | 这是平台适配边界。Windows 应映射到已注册的 MapGIS 检查、加载和制图工具，并实测返回结构。 |

`gee-remote-sensing` 是工具分类包；可供模型阅读的正文和附件属于 `gee-scripting` 工作流程包。只分配前者，模型可能有执行工具却读不到流程。影像取数、分析和脚本专家按需同时分配这两个包；需要自动准备中国行政区时还分配 `china-admin-boundaries`。服务开关与 agent 能力分配是两层设置。

默认配置只作为对照：应对队友现有 GDP、专题图或其他 agent profile 增量挂载，不要用本仓库默认 profile 整体覆盖 Windows 方案。技能内涉及 QGIS 工具的交付步骤也要改成 Windows 实际存在的 MapGIS 工具，不能仅替换软件名称。

## 3. 保留的运行语义与已修问题

### GEE

- `gee_check_environment`、`gee_run_script`、`gee_task_status`、`gee_cancel_task` 是四个入口。worker 在执行脚本前初始化 Earth Engine；脚本使用 `ee`、`output_dir`、`load_region`、`check_coverage`、`heartbeat`、`download_image`、`export_image_to_drive`，无需导入上游 `geocode`。
- 研究区读取验证 CRS、唯一要素和 Polygon / MultiPolygon 几何；按需重投影，不静默修复几何，不把行政区替换成估计矩形。没有本地中国行政区文件时，可读取已打包边界模板导出；同名或身份歧义返回候选，不猜第一条。
- 单日期模板有限评估候选日期、同日拼接、云掩膜、按完整 ROI 计算有效覆盖。覆盖函数返回浮点值，不是字典。低覆盖应如实报告并遵从任务要求，不能擅自不断扩大条件和重复下载。
- `gee_run_script` 的配置超时是 **连续无 stdout / stderr 输出的时长**，默认300秒；从子进程成功启动后开始计时，日志截断不影响续时。心跳可保持活跃，但用户取消和父任务截止仍有效。环境检查、任务状态、取消操作仍使用单次总时限。
- 下载固定使用 geedim 2.0.0 自动分块并拼成完整 GeoTIFF；不需要模型手工把研究区切成南、中、北反复下载。完成后检查文件与网格、波段、精度等；核对覆盖时使用原 ROI，不能把投影外接框中 ROI 外的空白算作数据丢失。
- 同会话中，影像表达式、region、scale、CRS、dimensions、dtype 相同且完成文件未变化时，允许返回原文件路径，`reused=true`；跨进程同请求有锁协调。调用方必须使用实际返回路径。参数变化、文件损坏或删除、未完成下载不会复用；不跨会话，不恢复未完成瓦片，不自动登记旧版遗留影像。`reuse=False` 可明确重取。
- Drive 导出是提交、记录任务 ID、查询或取消的异步路径；没有自动等待并下载 Drive 文件。停止本地 Python 不会自动取消已提交的云端任务，也不能因等待而重新提交同一导出。

### 浏览器与公开网页

- Playwright MCP 每聊天独立会话，不继承日常浏览器的 Cookie / 标签页；需要登录、验证码或人工授权时交由用户处理。检查启动程序存在，不等于真实浏览器已经成功启动。
- 已修复无参数工具在中间层克隆后被序列化为 `arguments: null` 的问题：发送合法 `{}`。普通本地参数错误、结构化工具 / JSON-RPC 请求错误保留浏览器会话；取消、超时、服务关闭、进程退出或传输故障才进入关闭重建路径。合并时不要遗漏 `manager.go` 与对应测试。
- 网页读取 / 下载只处理公开 HTTP(S) 地址，拒绝本机和内网目标；下载按流式字节数限量，失败清理未完成文件。它不会自动解压或保证下载内容科学有效。
- 浏览器、GEE 子进程是独立运行环境，不是操作系统安全沙箱。保持现有路径校验、会话输出目录约束、进程清理与用户取消，不要在移植时去掉这些边界。

## 4. 环境、配置和授权由接收方重新准备

**交付源码和公开运行资产，不交付开发者运行环境。** `autocarto_data`、实际运行时 `config/` 与 `data/`、模型密钥、Earth Engine OAuth 凭据、浏览器登录数据、会话记录、下载影像和本机绝对路径不应随此次交接提供。即使不在 `autocarto_data` 中，也不能因此把这些内容视为源码资产。

仓库中的配置结构、工具契约、默认模板可以保留；真实模型服务地址 / 密钥、GEE 项目、代理和解释器路径由队友在自己的部署环境配置。Earth Engine 凭据由官方库管理，不能写进 `integrations` 或提交到仓库。

Windows GEE 建议使用独立 Python 虚拟环境，**不要安装到 QGIS 自带 Python，也不要求安装到 MapGIS 的运行环境中**。已验证的 Python 包组合包含 `earthengine-api`、`geedim==2.0.0`、`geopandas==1.1.1`；历史本机 Earth Engine 版本记录为1.7.47，并不等于其他版本已经验证。具体创建虚拟环境、安装与授权命令见 [在线配置说明](online-data-and-gee.md)。

用户需要自行：

1. 准备获准使用 Earth Engine 的 Google 账号及项目，完成项目注册 / API / 权限设置。
2. 在选定的独立 Python 环境运行官方认证并在浏览器中完成授权；填写该解释器路径和自己的项目 ID。
3. 保存后执行环境检查，分别确认查询 `ready`、区域读取 `region_ready`、本地下载 `download_ready`，不能只看 HTTP 请求成功。

浏览器默认依赖 Node.js 18+、`npx`、已安装的 Chrome；MCP 固定为 `@playwright/mcp@0.0.83`。Edge / Chromium 需接收方自行准备。首次调用 `npx` 可能获取 npm 包；默认环境检查本身不安装或启动服务。Windows 对常规 `npx.cmd` 有专门适配；其他批处理启动器不直接通过 shell 执行，必要时配置 `node.exe` 及 MCP `cli.js` 参数。GEE 和浏览器可以各自启用，登录时不要启用无界面模式。

配置接口沿用 `GET /api/config`、`PUT /api/config/batch`；保存完整 `integrations` 对象后，显式调用 `POST /api/integrations/check`。批量保存失败必须保留用户输入并停止后续检查。开启服务不应自动替用户授权，也不应覆盖现有模型和 agent 配置。

## 5. 构建与 Windows 接入顺序

本仓库构建入口是根目录 `build.sh` / `build.bat`，模块要求以 `carto_agent/go.mod`、`autocarto-web/package.json` 和锁文件为准。当前 Go 模块声明 `go 1.24.0`、工具链 `go1.24.11`；前端是 React 19 / Next.js 15。桌面 Tauri 构建还涉及 Rust 和 Windows MSVC 工具链，Go SQLite 所需的本机构建能力也应按目标工程确认。

**不要在 MapGIS 工程直接把 `build.bat build` 当作通用发布命令。** 该脚本包含 QGIS 插件部署、sidecar 打包、固定的 Windows 运行目录 / 构建目录设置；`internal/config/paths.go` 也有 Windows 默认运行目录。需先审阅并适配这些位置，显式配置 `AUTOCARTO_HOME`（接收方可写运行目录）与 `AUTOCARTO_ASSETS_ROOT`（包含 `skills/system` 的运行资产根）。本交接不改写队友现有构建链。

建议接入顺序：

1. 在 Windows Go 工程合并 config、integrations、三个运行时及 API，完成工具 registry / descriptor 路由；先用独立测试目录验收，不连接真实制图工程。
2. 打包工具 YAML、workflow、参考、模板、行政区数据与许可。检查已分配专家能读取技能附件，主智能体不会绕过能力范围直接获得全部工具。
3. 合并 Web 设置、API 类型、错误显示、进度；验证保存后确实由新后端处理。刷新页面不等于替换旧 sidecar，更新需结束旧任务并重启实际服务。
4. 分别测试公开页面读取、浏览器打开 / 关闭、GEE 环境和小区域下载；通过后才接到 MapGIS 栅格检查、加载、符号化、布局和导出。
5. 测试停止、错误恢复、删除会话及服务退出。删除会话应等待本地执行器退出并阻止迟到保存；外部 GIS 工程引用的文件、复制到会话外的文件和已提交云任务不能一概视为已删除。

## 6. 已验证的范围与接收方需要复测的内容

以下是此前本机验证记录的摘要，详细条件见 [在线配置说明的验证章节](online-data-and-gee.md)。**本次编写交接文档没有重新运行全部测试，也没有在 Windows MapGIS 环境做端到端验收。**

| 环节 | 已有证据 | 不能据此推断 |
| --- | --- | --- |
| GEE 运行时 | 离线覆盖子进程、输出截断、空闲超时、父取消、下载失败、完成文件复用、任务日志恢复等路径；存在对应 Go / Python 测试 | 任意网络、账号、配额和大区域都能成功，或所有平台进程管理已实测 |
| 行政区 | 完整源哈希和3300个几何核验；唯一身份、重名、CRS、输出保护及实际区域读取测试 | 任意行政区名称都能无歧义，或原数据永远最新 |
| 真实 GEE | 已有小范围 Sentinel-2 查询、同日拼接、覆盖检查、10米 RGB GeoTIFF 下载及第二次进程复用原文件验证 | 整市固定耗时、所有影像算法 / 分类器 / 变化检测和 Drive 导出都已验证 |
| GIS 加载 | 本机 QGIS 3.44.10 独立无界面进程经真实工具引擎加载测试栅格并核验波段、CRS、像元 | 当前用户 QGIS 可视工程、WebSocket 全链和 Windows MapGIS 均已通过 |
| 智能体路由 | 真实模型曾走通空工程到技能读取、行政区身份选择与可信模板导出；另有确定性路由测试 | 该回归由模型自主完成整市遥感下载；当时 EE / QGIS 部分工具被拦截，实际只执行可信边界模板 |
| 浏览器 | 已有真实浏览器功能使用反馈；最新空参数 / 会话保留修复通过模拟 MCP、传输失败、取消及竞争检测测试 | 最新修复已在接收方所有浏览器版本与登录网站重新实测 |
| Web 设置与清理 | 已有配置、设置 API、进度、会话取消 / 删除的针对性回归 | 本仓库完整测试套件及所有既有 GIS 功能无回归 |

接收方可先在 `carto_agent/` 执行针对性离线检查：

```powershell
go test ./internal/geeruntime ./internal/browserruntime ./internal/webdata ./internal/integrations -count=1
go test ./internal/config ./internal/hitl ./internal/deepagent ./internal/runtimecontrol -count=1
go test ./cmd/agent -run 'TestRegisteredHITL|TestPrepareAgentTools|Test.*Skill' -count=1
go test ./internal/websocket -run 'Test.*GEE|Test.*Gee|Test.*Integration|Test.*DeleteSession|Test.*CancelSession' -count=1
```

测试中有 Python 可用性和平台条件，必须检查 `SKIP` 原因；跳过不等于通过。真实 Playwright smoke 默认不启用，需在明确准备好浏览器后按测试文件的开关执行。不要为了跑测试自动安装浏览器或复用开发者账号。

前端安装锁定依赖后，在 `autocarto-web/` 执行针对性设置测试，再按目标工程既有流程完成类型、构建与集成检查：

```powershell
npm ci
npm test -- src/hooks/integrationsSettings.test.tsx src/lib/integrationsApi.test.ts
```

最终应补一个接收方自己的真实验收：小 ROI 下载一次 → 用返回路径加载 MapGIS → 同请求再次执行返回复用路径 → 取消一个进行中的任务 → 确认没有残留执行器、重复影像或重复图层。记录实际版本、耗时和结果，避免将本机记录当作 Windows 验收报告。

## 7. 明确未实现、未完成的部分

- **尚未接入 GeoCode 右侧完整文件树与内嵌地理地图预览。** 当前日志、结果文件路径或图片展示不等于该能力。上游是 SolidJS / Vite；本前端是 React / Next.js，不能直接复制 UI 组件。上游还依赖工作目录列举、文件变化事件、GDAL 栅格预览缓存和 OpenLayers。上游地图预览也不能被表述为“GEE 下载像素实时流入页面”。
- **重复加载图层和双重标注问题尚未修复 / 验收。** 下载器复用完成文件只解决同请求重复落盘，不会自动阻止 GIS 中重复添加同一路径图层，也不会自动合并标注状态。Windows 需分别检查图层身份 / 路径去重、任务重试和标注重复应用，不能把文件复用当作已解决这些问题。
- 未移植 GeoCode 专用 OSM 专家和 `OsmSearch / OsmProbe / OsmGetObjects / OsmGetFeatures` 完整链。通用浏览器 / 网页下载不等于这些工具已经具备。
- 未移植 GeoCode 整套技能商店、所有专题制图脚本或前端交互。GEE 参考文档包含算法示例，不代表全部云端算法已运行验证。
- 本轮不承诺自然语言规划不再犯错，也不承诺大区域请求必定快于人工下载。出现问题时先区分模型规划耗时、Earth Engine 计算、网络传输、GIS 加载与制图；用工具日志和实际文件判断，避免无证据重新下载。

交接完成的标准是：队友能按上述模块定位代码，使用自己的配置和授权运行小范围测试，并明确知道 MapGIS 适配与剩余问题的边界。源码可读和构建成功只是其中两个环节。
