# AutoCarto MapGIS 版转换方案

> 编写日期：2026-10-29。
>
> 依据两份代码：
> - 本仓库 enio2 的当前工作区。v7 已实施，938 处改动尚未提交，位于 `refactor/v3` 分支。
> - 第一版 MapGIS 移植 `/home/workspace/Projects/enio-mapgis`。它没有 git 历史，文件日期为 2026-10-09，分叉点接近 enio2 提交 `77710c4`。
>
> 本目录可以直接交给执行智能体：先读本文件，再读所分配任务卡（`08-任务卡.md`）的“必读”规范。
>
> 简称约定：文中“第一版 README”或“插件 README”指 `enio-mapgis/autocarto_mapgis/README.md`；“第一版根 README”指 `enio-mapgis/README.md`。

## 0. 文档清单

| 文件 | 内容 |
|---|---|
| `README.md`（本文件） | 思路、现状与差距、目标设计、阶段、验收、约束、待确认事项 |
| `01-总体架构与协议.md` | 组件分工、宿主描述 `host.yaml`、运行时分发、握手与回执、图层引用与物化、标识与坐标约定 |
| `02-工具转换总表.md` + `tool-mapping.yaml` | 342 份契约和 7 个新增工具的逐项去向、保真度、实现位置、任务卡。YAML 是唯一依据 |
| `03-MapGIS宿主实现规范.md` | C# 插件结构、QGIS→MapGIS 概念对照、已验证接口、能力探查清单、各工具族实现要点、禁忌 |
| `04-geo运行时规范.md` | 与宿主无关的 Python 地理处理运行时：环境、进程协议、参数类型、物化与发布、表达式方言、算法对照、参照输出测试 |
| `05-后端前端与桌面改造.md` | Go 后端、前端、Tauri、构建脚本的宿主中立化清单（附文件与行号） |
| `06-资源工具包与流程迁移.md` | 样式 `style.yaml`、QML 转换、MapGIS 符号包、版面、路线资源、内置图层、38 个工具包、流程、提示词、文档 |
| `07-验证与发布.md` | 测试分层、对照与真机联调、固定场景、真实回放、安装包、证据格式 |
| `08-任务卡.md` | 53 张任务卡、依赖、泳道、文件归属、每卡步骤、验收与测试命令 |
| `evidence/` | `00-对比清单.md`（盘点与耦合点）、`mapgis-api-参考.md`（已验证接口）、各卡验证记录 |

## 1. 思路

一句话：**同一套 Go 后端和前端，新增一个 MapGIS 宿主包。** 只有必须在 MapGIS 里做的事（工程、图层、符号、标注、版面、导出、路线绘制）用 C# 插件实现；分析与数据处理放进与宿主无关的 Python 地理处理运行时（下称 geo 运行时）。工具名、参数和返回结构与 QGIS 版保持一致。

| 你的要求 | 方案的做法 |
|---|---|
| 全部工具、全部内容都要转 | 342 份契约逐项定去向（`02` 和 `tool-mapping.yaml`）：<br>- 96 项由 MapGIS 宿主实现（含 7 个新增）；<br>- 204 项由 geo 运行时实现；<br>- 44 项 Go 内建、2 项独立进程，原样沿用；<br>- 3 项确实无法提供，写明原因。<br>工具包、流程、样式与版面资源、内置图层、提示词和文档同样逐项迁移（`06`） |
| 软件支持不一样，有的无法完全转换 | 每项标注保真度：等价 215、近似 57、降级 25、不支持 3。近似和降级的差异写进 MapGIS 版契约说明，调用时不静默忽略。需要 SDK 实测才能定的 19 个问题先在 M02 探查，再写实现 |
| 参考 MapGIS 文档写工具函数 | 公开网络上没有 MapGIS 10 .NET 的完整 API 参考（司马云开发包需登录下载）。分三层取证：<br>1. 以第一版已在真机验证的调用为准（`evidence/mapgis-api-参考.md`）；<br>2. 以中地数码公开的 MapGIS 移动端 SDK 文档印证对象模型（Theme/RangeTheme/UniqueTheme、SpaAnalysis、SpaProjection、LabelInfo 等）；<br>3. 在 M02 用反射导出本机 MapGIS 程序集的全部公开接口，配合本机 SDK 帮助逐项确认 |
| 交给其他智能体执行 | `08` 中的 53 张卡分 7 条泳道，每张写明必读、涉及范围、步骤、验收和测试命令。C# 卡只能在装有 MapGIS 的 Windows 上执行；Go、前端、geo 和内容卡可以在任意平台先做 |

## 2. 现状与差距

### 2.1 两个项目对比

| 项目 | QGIS 版 enio2（当前工作区） | 第一版 MapGIS（enio-mapgis） |
|---|---|---|
| 架构 | 自由书写的流程 FLOW.md、计划 v2、统一数据服务、内置图层库、样式与版面资源库（v6/v7） | 固定角色方案加工作流技能包（相当于 v4 之前的形态） |
| 工具契约 | 342 份：<br>- `qgis` 300（178 个 Processing 算法别名、122 个 Python handler）；<br>- `internal` 40；<br>- `cli` 2 | 57 份：`mapgis` 47、`internal` 10 |
| 宿主实现 | Python 插件约 2.6 万行，加 QGIS Processing | C# 插件约 4 万行，47 个 handler，在 `AutoCartoRuntime.cs:78-134` 手工注册 |
| 工具包 | 38 个（`autocarto/skills/system`） | 26 个 |
| 流程 | 内置 2 份（行军路线图、统计专题图）；`docs/` 下另有 2 份（轨道交通站点覆盖、县域地形坡度分级） | 无，编排写在方案提示词里 |
| 资源 | 11 个 QML 样式、2 个声明式版面（`.layout.yaml`）、8 个内置行政区图层、路线样式与军标符号 | 纸张、图例、符号映射写死在 C# 里 |
| Go 后端 | 54 个包，400 个测试文件 | 47 个包，33 个测试文件（基线有 197 个） |
| 运行时 | `qgis` / `internal` / `cli` | `mapgis` / `internal` / `cli` |

### 2.2 第一版做对、要保留的部分

- **插件运行模型**：
  - 宿主完成初始化后才连后端（`AutoCartoRuntime.cs` 中的 `OnHostLoaded`、`OnHostIdle`）；
  - 工具调用进串行队列并切到 UI 线程执行；
  - 取消请求不中断正在执行的工具；
  - 心跳卡死时自动断开重连（`ToolInvokeRouter.cs`、`AutoCartoRuntime.cs`）。
- **原生崩溃防护**：带 `HandleProcessCorruptedStateExceptions` 的 `UiDispatcher.RunGuarded` 与 `NativeGuard`。去掉它，MapGIS 内部一个访问冲突就会让整个程序退出（见 `UiDispatcher.cs` 注释中的实测）。
- **宿主文档归属**：直接使用 `IApplication.Document`，插件从不 Save、Close 或 Dispose 宿主文档；打开、新建过程中出现的短暂空路径不判为关闭（`DocumentSession.cs`、`ProjectPathStability.cs`）。
- **已在真机验证的接口用法**：
  - 矢量先 `DataConvert` 入本地库再按类 URL 挂图层；栅格用 `RasterDataSet` 加 `RasterLayer`；
  - 版面导出用 `OutputToPDFFile` 和 `OutputToStream`；
  - 标注用 `LabelInfo`、`SimpleLabel`、`TextAnnInfo`；
  - 系统库颜色用 `AddColor`；坐标系用 `SRefData.EPSGToSRS` 和 `WKTToSRS`；
  - 符号库用 gb2312 编码的 XML 安装。

  全部摘要见 `evidence/mapgis-api-参考.md`。
- **后端能力**：另存为时迁移会话目录、实例断连时立即让挂起的调用失败、显式目标实例的重连等待（enio-mapgis `provider.go:173,595,1085`、`session_project_move.go`）。
- **路线绘制**：纯 C# 贝塞尔与 Flox 内核（`Handlers/Support/Plot/`），加独立 Java 后端（`assets/route-flox/backend.jar`）。

### 2.3 第一版的问题（这次必须避免）

1. **场景内容写死在工具里**，换任务就不能用，与流程化的方向相反：
   - 专题版面固定 1000×855 毫米、比例尺 1:700 万（`LayoutCreateTool.cs:35,68`）；
   - 图例固定，条目里写着“红25军长征路线”（`FixedLegendCatalog.cs:54`）；
   - 字段值到符号名的对应写在代码里（`LayerSetLineSymbolTool.cs:28-42`）；
   - 路线版面固定 1360×940 毫米。
2. **纯文件逻辑重写了一遍 C#**：路线校验 `RouteValidator.cs` 1619 行、编译 `RouteCompiler.cs` 905 行、专题数据准备 1178 行。enio2 中的 `autocarto/knowledge/` 和 `_shared/statistical_data_impl.py` 都不导入 QGIS，两份实现必然漂移。
3. **全局改名**：runtime、服务键、配置键、实例类型和工程工具名（`open_mapx` 等）都改了，Go 和前端随之多处硬编码。QGIS 版每演进一次，就得重新移植一次。
4. **工具注册写死**：插件不读后端下发的工具清单（`ToolInvokeRouter.cs:276` 的 TODO）。只加 YAML、不加 C#，调用时才报未实现。
5. **没有图层 ID**，全按名称查找。当前 enio2 的系统流程依赖 `layer_id`，例如尝试清理（`flowrun/attempt_layers.go:87`）、取数检查（`websocket/plan_acquisition_v2.go:329`）、样式读回（`flowrun/verify_symbology.go:16`）。
6. **样式写进要素图形参数**（`LayerToolSupport.cs:1099,1285` 的 `cls.UpdateInfo`），同一数据源无法有两种样式。
7. **启动与路径**：
   - 未设 `AUTOCARTO_HOME` 时，回退配置和日志用的是 MapGIS 程序目录（`RuntimePortReader.cs:25`、`AutoCartoRuntime.cs:54`），与注释所说的插件目录不符；
   - `AutoCarto-MapGIS.exe` 需要管理员权限，经 UAC 提权启动时，桌面壳注入的环境变量可能到不了插件（enio-mapgis `src-tauri/src/lib.rs:613-650`）。
8. **测试缺失**：Go 测试文件从 197 个删到 33 个，C# 没有测试工程。

### 2.4 当前 enio2 中与 QGIS 绑定的位置

完整清单见 `evidence/00-对比清单.md`，这里只列类别：

- **Go 常量与类型**：
  - runtime 常量 `tool/types.go:17`、`skill/backend.go:11`；
  - `skill/qgis_backend.go`；
  - 实例类型与服务名 `discovery/types.go:6-21`，SQLite 表 `discovery/registry.go:492,559`；
  - 配置段 `config/config.go:56` 及白名单 `config/config_service.go:734,740`；
  - 资产根候选 `config/paths.go:43`；
  - 握手字段 `middleware/bridge/provider.go:590`。

  共 68 个非测试 Go 文件出现 QGIS 字样。
- **Go 运行环境**：
  - 计算环境要求能 `import qgis.core`（`cmd/agent/main_deep.go:1222,1263`），并推导 QGIS 环境变量（`compute/runtime.go:116,149`）；
  - 私有瓦片按 QGIS 认证配置处理（`middleware/bridge/private_tiles.go`）；
  - 按工具包名判断步骤是否需要宿主（`flowrun/attempt_layers.go:96`）；
  - OSM 精裁走宿主（`integrations/osm_clip.go:215`）。
- **Go 直接按名称调用宿主工具**：28 个非测试文件、80 处，涉及 `get_layers_info`、`layout_info`、`layout_list`、`layer_remove_created`、`layer_inspect`、`resource_preview`、`layout_save_template`、`layer_save_style`、`geodata_*`、`data_normalize`、`data_derive`、`admin_boundary_extract`、`data_load_tiles` 等。
- **资源格式**：QML/QPT 规则见 `resource/manage.go:515`、`resource/resolve.go:465`、`resource/preview.go:15`、`sessionfiles/classify.go:30`。
- **前端**：41 个非测试文件，例如：
  - 配置键 `qgis.*`（`src/lib/api.ts:1829-1849`）；
  - 服务键 `services.qgis`（`src/lib/serviceStatus.ts:270`）；
  - 实例字段 `qgis_available`（`src/lib/api.ts:162`）；
  - 工程扩展名 `.qgz`（`src/hooks/useQGISProjectLauncher.ts:414`）。
- **桌面壳与构建**：
  - Tauri `src-tauri/src/lib.rs` 共 65 处，包括检测、启动和 macOS profile 同步（539–951 行）；
  - `build.bat:17,505-516` 把插件部署到 QGIS profile。
- **资产**：
  - 307 份工具契约写有 qgis、qml 或 qpt；
  - 工具包中 62 个文件提到 QGIS、QML 或 QPT（含参考文档），其中 `pyqgis-scripting` 占 14 个文件、86 处；
  - 11 个 QML 样式。

## 3. 目标设计

### 3.1 总图

```text
autocarto-web（Next.js + Tauri，读 host.yaml 决定宿主名称、工程格式、启动方式）
        │ HTTP / WS
carto_agent（Go 控制面，宿主中立）
   ├─ 宿主后端（runtime qgis | mapgis）── 反向 WS ──► MapGIS 插件（C#，autocarto_mapgis/plugin）
   │                                                 工程 · 图层 · 选择集 · 符号与标注 · 版面 · 导出 · 路线绘制
   ├─ geo 后端（runtime geo）── 进程池 ──► Python 地理处理（autocarto_mapgis/geo，conda 环境）
   │                                                 几何 · 字段 · 栅格 · GDAL · 网络 · 点云 · 图表 · 取数处理 · 统计 · 路线知识
   ├─ internal（Go 进程内）：数据服务、计划、OSM、网页/浏览器、GEE、地理编码、文件、创建器 …
   └─ cli（独立脚本）：质量审查准备与读图
```

数据流：geo 工具需要的宿主图层，由系统先调用宿主 `layer_export` 物化为会话临时文件。geo 的输出文件由系统调用宿主 `data_load_*` 或 `layer_replace_source` 发布回工程，再把真实 `layer_id` 写回回执（`01` §6）。模型看到的仍是与 QGIS 版相同的工具和回执。

### 3.2 什么放在哪里

| 放在 | 判定 | 例子 |
|---|---|---|
| MapGIS 宿主（C#） | 需要读写宿主对象：文档、地图、图层、选择集、专题、标注、版面元素、视图、导出 | `open_project`、`data_load_vector`、`layer_set_symbology`、`layout_compose`、`export_layout_pdf`、`route_draw` |
| geo 运行时（Python） | 输入、输出都能是文件的计算，不需要宿主渲染 | 全部矢量几何与字段工具、GDAL、栅格、网络、点云、图表、`data_normalize`、`admin_boundary_extract`、`marching_route_*` |
| Go 内建 | 联网、控制、数据服务、计划、会话 | 原样沿用；`geocode_*` 和 `file_downloader` 改为 Go 内建，复用网络策略与来源登记 |

这样分的理由：

1. MapGIS 10 的 .NET 空间分析接口没有公开文档，是几何级接口（`SpaAnalysis.buffer`、`intersection` 等），且以本地库要素类为单位，逐个封装成本高、容易触发原生崩溃、无法离开 Windows 测试。
2. geo 运行时与 QGIS Processing 用的是同一套 GDAL/GEOS，结果可以和 QGIS 版逐项对照（V71），可以在 Linux CI 上测试。以后 QGIS 版新增分析工具，MapGIS 版也能直接复用。
3. 取数阶段的规范化和精裁不再依赖宿主在线，MapGIS 未启动时也能准备数据。

### 3.3 同一仓库，两个宿主包

```text
enio2/
├── autocarto/            QGIS 宿主包（原样，只新增 host.yaml）
├── autocarto_mapgis/     MapGIS 宿主包（新增）
│   ├── host.yaml         宿主描述（01 §2）
│   ├── plugin/           C# 解决方案（03）
│   ├── geo/              geo 运行时 Python 包与 environment.yml（04）
│   ├── tools/system/     工具契约（由 R60 依 tool-mapping.yaml 生成，再人工补齐说明）
│   ├── skills/system/    工具包
│   ├── flows/system/     内置流程
│   ├── resources/system/ 样式（style.yaml）与版面（layout.yaml）
│   ├── data/layers/      内置图层库（数据文件可与 QGIS 包共用）
│   └── symbols/          MapGIS 符号包（gb2312 XML）与原始 SVG
├── carto_agent/          Go（宿主中立化）
└── autocarto-web/        前端与 Tauri（宿主中立化）
```

`AUTOCARTO_ASSETS_ROOT` 指向哪个包，就是哪个宿主的发行版。Go 与前端只认 `host.yaml`，不再写死 QGIS 或 MapGIS。

### 3.4 工具一致

- **工具名不变**：
  - `open_project`、`create_project`、`get_layers_info` 等全部沿用，Go 和流程不需要映射表；
  - 只有 6 个 `pyqgis_*` 改名为 `geo_*`，因为语义变了；
  - MapGIS 特有的 `save_project`、`add_map`、`get_map_info` 作为新增工具提供。
- **参数不变**：YAML 参数逐项沿用。Processing 风格的大写参数名和枚举序号，也按 QGIS 契约原样接受。
- **返回结构不变**：宿主工具的 `output`/`info` 字段与 QGIS 版逐字段对齐，由契约样例校验（P33、V70）。版面坐标对外统一用 QGIS 约定：页面左上角为原点，y 向下，单位毫米。MapGIS 内部左下角原点的换算只在插件里做（`01` §7）。

### 3.5 资源格式

| 资源 | QGIS 版 | MapGIS 版 |
|---|---|---|
| 样式 | QML | `*.style.yaml`：参数化符号与标注，与 `layer_set_symbology` 的参数同构。复杂标准符号（例如铁路）引用 MapGIS 符号包中的符号名 |
| 版面 | QPT 或 `*.layout.yaml` | 只用 `*.layout.yaml`（声明式）；两个内置版面本来就是这种格式 |
| 符号 | SVG、QGIS 样式库 | MapGIS 系统库符号（gb2312 XML 符号包，按文件指纹安装），保留原始 SVG |
| 预览 | QGIS 离屏渲染 | MapGIS 离屏文档渲染；M02 探查不可用时，由 geo 生成示意图并标注“示意” |

### 3.6 不支持与降级怎么处理

- **不支持**的工具（`cartography_25d_style`、`layout_duplicate`、`gps_download`）不进入 MapGIS 工具包，能力清单写明原因；流程和工具包里不再提及。
- **降级**的工具在契约说明里写清支持范围。超出范围的调用返回明确的错误码和说明，例如 `unsupported_parameter`，不静默忽略。
- 依赖 M02 探查结果的项（混合模式、版面元素旋转、在线瓦片、离屏预览、宿主退出等）：探查通过就按等价或近似实现，不通过就按 `02` 表中的降级方案实现。结论记录在 `evidence/M02-probes.md`。

## 4. 转换原则

1. **只转换，不重新设计业务。** 流程、计划、数据服务、执行器的行为与 QGIS 版一致；本轮不改 v7 的产品逻辑。
2. **宿主差异收在三处**：`host.yaml`、C# 插件、MapGIS 包里的资产。Go 和前端代码里不出现 `if mapgis`，只读宿主描述与契约。
3. **工具不写场景**：纸张、图例条目、字段值到符号的映射、颜色方案只来自参数、流程和资源。
4. **一份逻辑只有一份实现**：纯 Python 的知识编译、统计准备直接复用 `autocarto/` 中的代码。只有表达式引擎例外，C# 与 Python 各一份，用共享用例保证一致（P34、G51）。
5. **先探查后实现**：涉及 SDK 行为不确定的地方，先写探查程序在真机验证，再写工具；探查结论进证据。
6. **原生调用一律防护**：只在 UI 线程执行，全部经 `NativeGuard`；宿主文档只在工程类工具中按明确语义打开、新建、保存。
7. **结果可对照**：geo 工具与 QGIS 参照输出对照；宿主工具与 QGIS 回执结构对照；场景与流程画法读回对照。
8. **QGIS 版不受影响**：QGIS 包只新增 `host.yaml`；Go 和前端的中立化保持 QGIS 行为不变，以 `./build.sh check` 为准。

## 5. 阶段与任务

详见 `08-任务卡.md`。

| 阶段 | 任务 | 做完后 |
|---|---|---|
| 0 基线与环境 | M00 基线冻结；M01 Windows 与 MapGIS 环境；M02 接口清单与能力探查；M03 geo 环境 | 有可回滚的基线；SDK 能力不再靠猜 |
| 1 宿主中立化 | H10 `host.yaml`；H11 运行时与可用性；H12 握手与桥接；H13 配置与环境；H14 系统内部宿主调用；H15 资源多格式；H16 geo 运行时（Go）；H17 地理编码与下载内建；W20 前端；W21 桌面壳；W22 构建与 CI | Go、前端、桌面壳能同时承载 QGIS 与 MapGIS |
| 2 插件骨架 | P30 解决方案与运行时移植；P31 协议与注册；P32 宿主对象适配层；P33 测试基座与真机联调；P34 表达式引擎（C#） | 插件连得上，有图层 ID、坐标系、颜色、字体、本地库等公共能力 |
| 3 宿主工具 | T40 工程；T41 数据加载；T42 画布；T43 选择集；T44 符号与标注；T45 版面元素；T46 版式合成与导出；T47 图文标注；T48 路线绘制；T49 资源预览 | 96 项宿主工具可用 |
| 4 geo 工具 | G50 骨架与协议；G51 表达式；G52 矢量与转换；G53 字段与提取；G54 栅格与地形；G55 统计、网络、点云、图表；G56 取数、统计与路线知识；G57 脚本工具 | 204 项 geo 工具可用并与 QGIS 对照 |
| 5 内容 | R60 契约包；R61 样式；R62 版面；R63 路线资源；R64 内置图层；R65 工具包；R66 流程；R67 提示词；R68 文档 | MapGIS 包内容完整 |
| 6 验收 | V70 一致性门禁；V71 geo 对照；V72 真机冒烟；V73 固定场景；V74 真实回放；V75 安装包 | 可以发行 |

```text
M00 → M01 → M02；M00 → M03
H10 ← M00   H11 ← H10   H12 ← H11   H13 ← H10   H14 ← H11, H16   H15 ← H10   H16 ← H11, H13   H17 ← H11
W20 ← H10, H12   W21 ← H10, H13, M01   W22 ← W21, P30, M03
P30 ← M01   P31 ← P30   P32 ← P31, M02   P33 ← P31   P34 ← P30
T40 ← P32, P33   T41 ← T40, H16   T42 ← T40   T43 ← T41, P34   T44 ← T41, P34
T45 ← T40   T46 ← T45   T47 ← T46   T48 ← T44, T46   T49 ← T44, T46
G50 ← M03（与 H16 并行开发，联调验收依赖 H16）   G51 ← G50   G52、G53 ← G51   G54、G55、G56、G57 ← G50
R60 ← H11   R61 ← H15, T44   R62 ← T46   R63 ← T48   R64 ← T41, R61   R65 ← R60   R66 ← R61, R62, R65   R67 ← H10   R68 ← R66
V70 ← R60, R65, R66, H14, W20   V71 ← G52–G57   V72 ← T40–T49, V70   V73 ← V72, R66   V74 ← V73   V75 ← W22, V73

关键路径：M00 → M01 → P30 → P31 → P32 → T40 → T41 → T44 → T48 → V72 → V73 → V74（M02 须在 P32 之前完成）
```

## 6. 验收标准

1. **Y1 覆盖**：`tool-mapping.yaml` 覆盖全部 342 份契约。MapGIS 工具包中的每个工具，要么有实现且在目录中可用，要么在能力清单中标为不提供并写明原因。由 V70 自动检查。
2. **Y2 一致**：
   - 宿主工具与 geo 工具的参数与 QGIS 版一致（改名的 6 个脚本工具除外）；
   - 宿主工具回执通过契约样例校验；
   - Go 代码中按名称调用的宿主工具（`evidence/00-对比清单.md` §3）全部可用。
3. **Y3 geo 对照**：保真度为“等价”的 geo 工具与 QGIS 参照输出一致：要素数相同，属性相同，面积和长度相对差不超过 0.1%，栅格逐像元差不超过数据精度。“近似”工具的差异写进 V71 记录。
4. **Y4 真机冒烟**：除“不支持”外的 96 项宿主工具，各在真实 MapGIS 中至少成功执行一次；全程没有宿主崩溃或卡死。
5. **Y5 固定场景**：5 个固定角色场景在真实 MapGIS 上导出 PDF 和 PNG：行军路线 A1、行军路线 A4、统计专题图、轨道交通站点覆盖、县域地形坡度分级。符号读回与流程画法一致：颜色相同，线宽误差 ±0.01 毫米，标注字段与开关相同。
6. **Y6 真实回放**：4 个任务用真实模型、真实网络、真实 MapGIS 完整出图，记录耗时、工具调用和失败次数：行军路线图、河南省人口统计专题图、郑州地铁站点覆盖图、一个县的坡度分级图。
7. **Y7 QGIS 不回退**：`./build.sh check` 通过；QGIS 版的固定场景结果不变。
8. **Y8 门禁**：
   - Windows 上 `build.bat check` 通过：Go、Web、C# Core 单元测试、geo 测试、插件编译；
   - Linux CI 跑 Go、Web、C# Core、geo。
9. **Y9 不写死场景**：C# 工具代码中没有纸张尺寸、图例条目、字段值到符号名的映射、写死颜色方案等常量。由 V70 的扫描规则检查，例外须登记。
10. **Y10 安装**：干净的 Windows（已装 MapGIS）按文档安装，30 分钟内能做出第一张图。

## 7. 全局约束

1. 开工前由用户确认并提交 enio2 当前工作区的 938 处改动，从该提交建 `mapgis/main` 分支。不替用户丢弃或覆盖未提交的改动。
2. 工具名、参数、返回结构以 QGIS 版为准。有差异时先改 `tool-mapping.yaml` 并写明原因，再改代码。
3. 工具里不写场景内容（见原则 3 与 Y9）。
4. 插件不 Save、Close 或 Dispose 宿主文档，`save_project`、`open_project`、`create_project` 按其明确语义操作的情况除外。
5. 原生调用全部在 UI 线程、经 `NativeGuard`；捕获访问冲突后，不假定原生状态仍可安全重试。
6. 用户原始数据和内置图层只读，派生结果写入会话目录。导入本地库的副本属于派生数据。
7. 测试使用临时运行目录，不读写真实的 `autocarto_data/`；单元测试和契约测试不联网；真实回放单独记录。
8. 验证记录写在 `docs/refactor-mapgis/evidence/<卡号>.md`；提交信息以卡号开头（例如 `T44: 按参数设置 MapGIS 图层符号与标注`）；在 `mapgis/main` 提交，不推送、不合并。
9. QGIS 包 `autocarto/` 只允许 H10 新增 `host.yaml`，其他改动须先经用户确认。geo 运行时只读导入 `autocarto.knowledge` 与 `_shared/statistical_data_impl.py`。
10. 编码约定：
    - YAML、Markdown、源代码用 UTF-8；
    - MapGIS 符号 XML 用 gb2312，读写时必须显式指定；
    - Shapefile 属性编码按 `.cpg`，缺省按 GBK；
    - Newtonsoft.Json 钉在 13.0.3（宿主自带版本）。

## 8. 交给执行智能体时的提示词

```text
你是本仓库的执行智能体，请完成任务 Xnn。
1. 先读 docs/refactor-mapgis/README.md（特别是 §1、§3、§4、§7），再读 docs/refactor-mapgis/08-任务卡.md 中该任务的卡片，以及卡片“必读”列出的规范与证据。
2. 工具的去向、保真度与实现位置以 docs/refactor-mapgis/tool-mapping.yaml 为准；与实际代码不一致时，以实际代码为准，并在验证记录中写明对应关系。
3. 只修改卡片“涉及范围”中的文件。涉及 MapGIS SDK 行为时，先查 docs/refactor-mapgis/evidence/M02-probes.md；没有结论的，先补探查再实现。
4. 完成后运行卡片中的测试命令，按 docs/refactor-mapgis/evidence/README.md 写 evidence/<任务号>.md，在 mapgis/main 分支提交（提交信息以任务号开头，不推送）。
5. 不要开始其他任务。
```

## 9. 待确认事项

| # | 事项 | 建议 | 备选 | 决定 |
|---|---|---|---|---|
| 1 | 仓库形态 | 同一仓库新增 `autocarto_mapgis/` 宿主包，Go 与前端宿主中立化（§3.3） | 像第一版那样另建分叉：初期快，但 QGIS 版每演进一次都要重新移植 | |
| 2 | 分析类工具在哪执行 | geo 运行时（§3.2） | MapGIS 原生 C#：用 SpaAnalysis 等几何级接口逐个封装，工作量大，只能在 Windows 上测试 | |
| 3 | geo 环境 | Miniforge 加 conda-forge 固定版本环境：GDAL、PDAL 都来自同一套库（`04` §2） | pip 轮子 venv：没有 PDAL，多个轮子自带的 GDAL 可能冲突；或随安装包内置 Python | |
| 4 | 样式落地方式 | 图层专题（Themes），不改数据 | 写要素图形参数（第一版做法） | |
| 5 | 图层 ID 持久化 | SDK 有可持久的图层自定义属性就用它；没有则用工程旁的 `<工程>.autocarto.json` | 只按名称（第一版做法） | |
| 6 | 多版面 | 一个文档一个版面，加名称登记；`layout_duplicate` 不提供 | 用多个文档模拟多版面 | |
| 7 | 样式资源格式 | `*.style.yaml` 加 MapGIS 符号包 | MapGIS 原生样式文件（若 M02 发现有可用格式） | |
| 8 | 资源预览 | MapGIS 离屏文档渲染（M02 探查通过时） | geo 生成示意图 | |
| 9 | PyQGIS 的替代 | 只提供 geo 脚本工具（Python，文件级处理） | 另外提供 C# 宿主脚本（CodeDom 编译，默认关闭） | |
| 10 | 不支持的工具 | 从 MapGIS 工具包移除，能力清单写明原因 | 保留契约，调用时返回 unsupported | |
| 11 | 在线瓦片 | 只在 MapGIS 原生瓦片或 WMTS 图层探查通过时提供 | 下载拼接成栅格（可能违反服务条款，不建议） | |
| 12 | MapGIS 版本 | 以你实际安装的版本为准（第一版基于 MapGIS 10，默认路径 `E:\MapGIS 10\Program`），由 M01 记录版本号与 SDK 位置 | — | |
| 13 | 路线绘制内核 | 沿用第一版 C# 内核，适配当前数据集与样式资源 | 移植 QGIS 外部插件当前版本的 Python 内核 | |
| 14 | 表达式 | QGIS 表达式常用子集；C# 与 Python 各实现一份，共享用例（`04` §7） | 只在 Python 中求值，宿主工具由系统先调用 geo 求出要素编号 | |
| 15 | 发行方式 | 桌面安装包 + 插件包 + geo 环境安装脚本 | 一体化离线包，含 conda 环境，约 1.5 GB | |
