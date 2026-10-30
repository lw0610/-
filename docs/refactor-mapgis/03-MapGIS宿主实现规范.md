# 03 MapGIS 宿主实现规范

本文件约束 `autocarto_mapgis/plugin/` 中的全部 C# 代码。接口取证分三层：

1. 第一版在真机验证过的调用，汇总在 `evidence/mapgis-api-参考.md`；
2. M02 从本机程序集导出的接口清单（`evidence/mapgis-api/`）；
3. M02 的探查结论（`evidence/M02-probes.md`）。

三者都没有覆盖的接口，先补探查，再写实现。

## 1. 解决方案结构

```text
autocarto_mapgis/plugin/
├── AutoCarto.MapGIS.sln
├── AutoCarto.MapGIS.Core/            netstandard2.0；不引用任何 MapGIS 程序集，可在 Linux 上编译测试
│   ├── Protocol/                     消息、回执、错误码、工具注册特性（HostToolAttribute）
│   ├── Routing/                      串行队列与取消语义的纯逻辑部分
│   ├── Expressions/                  表达式方言解析与求值（P34，与 geo 共用用例）
│   ├── Layout/                       版式引擎几何：layout.yaml 解析、锚点、纸张、字号表、坐标换算
│   ├── Symbology/                    style.yaml 与 layer_set_symbology 参数模型、色带、分级断点、尺寸缩放
│   ├── Identity/                     图层 ID 登记与重新关联的纯逻辑
│   ├── Route/                        路线几何内核（第一版 Handlers/Support/Plot/*，原样迁入后去掉 MapGIS 依赖）
│   └── Json/                         Newtonsoft.Json 13.0.3 辅助
├── AutoCarto.MapGIS.Core.Tests/      net8.0 + xUnit；Linux CI 可跑
├── AutoCarto.MapGIS.Plugin/          net48 x64；引用 Core 与 MapGIS 程序集（Private=false）
│   ├── PluginEntry.cs、AutoCartoRuntime.cs
│   ├── Backend/                      反向 WS、实例上报、调用路由（移植第一版）
│   ├── Host/                         宿主适配层（P32）：HostSession、LayerRegistry、CrsMapper、ColorLibrary、FontLibrary、GdbWorkspace、SymbolPacks、MapViewSync、NativeGuard、UiDispatcher
│   └── Handlers/<族>/<工具>Tool.cs   每个工具一个文件，[HostTool("工具名")] 标注
└── tools/MapGisApiDump/              net8.0 控制台；用 MetadataLoadContext 只读导出 MapGIS 程序集的公开接口（M02）
```

csproj 沿用第一版：

- `MapGISDesktopDir` 属性（缺省 `D:\MapGIS\Program`，可用 `-p:` 覆盖）；
- `ValidateMapGISDesktopDir` 目标；
- MapGIS 引用 `Private=false`；
- Newtonsoft.Json 钉在 13.0.3（见第一版 csproj 注释：宿主运行时提供的就是这一版，用更新的 API 只会在运行时抛 `MissingMethodException`）。

插件资产不再复制进插件输出目录。插件通过 `host.yaml` 所在的资产根读取符号包与 Flox 后端。第一版要求把 `skills/`、`tools/`、`assets/` 拷到 `bin/`；现在由 `AUTOCARTO_ASSETS_ROOT` 或插件配置文件给出资产根，`instance_connect.skills_dir` 照常上报。

## 2. 运行时组件（移植自第一版）

| 组件 | 第一版文件 | 移植要求 |
|---|---|---|
| 插件入口 | `PluginEntry.cs` | 原样；`IPlugin` + `IConnect` |
| 运行时单例 | `AutoCartoRuntime.cs` | 去掉手工注册，改为反射扫描 `[HostTool]`；保留宿主就绪判定（`ApplicationLoadedEvent` / 可见 MDI 空闲）与工程状态采样定时器；去掉 SelfTest 的注释代码 |
| WS 客户端 | `Backend/BackendWsClient.cs`、`WebSocketTextMessageBuffer.cs` | 原样（重连指数退避，封顶 8 秒，发送锁与重发队列） |
| 实例上报 | `Backend/InstanceReporter.cs` | 增加 `host_type`、`host_version`、`plugin_version`、`implemented_tools`、`capabilities`（`01` §4） |
| 调用路由 | `Backend/ToolInvokeRouter.cs` | 原样语义（串行队列、取消规则）；执行完成后补 `output_layers`；超时取契约值 |
| 端口与配置 | `Backend/RuntimePortReader.cs` | **修正**：回退配置文件从插件 DLL 所在目录读取（`typeof(AutoCartoRuntime).Assembly.Location`），不用 `AppDomain.CurrentDomain.BaseDirectory`（那是宿主目录）；配置文件格式改为 `key=value`，至少含 `AUTOCARTO_HOME`、`AUTOCARTO_ASSETS_ROOT` |
| 日志 | `Support/FileLog.cs` | **修正**：未解析到运行目录时写 `%LOCALAPPDATA%\AutoCarto\logs\mapgis-plugin.log`，不写宿主目录；按大小轮转 |
| UI 调度 | `Support/UiDispatcher.cs` | 原样，保留 `HandleProcessCorruptedStateExceptions` |
| 原生防护 | `Handlers/Support/NativeGuard.cs` | 原样；访问冲突统一转为 `native_failure` |
| 宿主就绪 | `Support/HostStartupReadiness.cs` | 原样 |
| 当前工程 | `Handlers/Support/DocumentSession.cs`、`ProjectPathStability.cs` | 原样；扩展为 `Host/HostSession`（见 §4.1） |
| 视图同步 | `Handlers/Support/MapViewSync.cs` | 原样；画布工具（T42）复用 |
| 本地库 | `DataLoadVectorTool.cs` 中的共享连接 | 抽成 `Host/GdbWorkspace`；改用专用库 `AutoCarto`（`01` §8） |
| 线符号与点符号安装 | `Handlers/Support/LineSymbolLibrary.cs`、`RoutePointSymbolLibrary.cs` | 抽成 `Host/SymbolPacks`，按资产文件指纹安装；新增程序化生成虚线线型（M02 第 19 项） |
| 版面公共操作 | `Handlers/Support/LayoutSupport.cs` | 保留元素查找、分组、范围计算、主图视域同步；去掉“route_draw 拥有页面”的假设 |
| 路线内核 | `Handlers/Support/Plot/*`、`Route*.cs` | 几何部分迁入 `Core/Route`；落图部分留在 T48 |

不移植：

- `RouteValidator.cs`、`RouteCompiler.cs`、`RouteVocabulary.cs`、`RouteKnowledgeSupport.cs`、`RouteReviewSupport.cs`：路线知识改由 geo 运行时复用 `autocarto/knowledge`；
- `DataPrepareThematicTool.cs`：由 `data_prepare_statistical` 替代；
- `FixedLegendCatalog.cs`、`FixedLegendBuilder.cs`：写死图例；
- `LayoutCreateTool.HardcodedDefaults`：写死纸张。

## 3. QGIS → MapGIS 概念对照

| QGIS 版概念 | MapGIS 对应 | 约定 |
|---|---|---|
| 工程 `QgsProject`（.qgz） | `IApplication.Document`（.mapx） | 宿主文档归插件“借用”，不 Close/Dispose |
| 单一地图画布 | 文档含多张 `Map` | 工具默认作用于“当前地图”：活动视图的地图，否则第一张。没有地图时，`data_load_*` 自动 `add_map("地图1")`。所有回执带 `map_name` |
| 图层面板从上到下 | `Map` 图层序（`Append` 加到最上） | `get_layers_info` 按显示从上到下输出，与 QGIS 版顺序语义一致 |
| `layer.id()` | 无 → 插件登记 `ml_…` | `01` §7.1 |
| OGR 矢量图层 | 本地库要素类 + `VectorLayer`（`MapLayerHelp.CreateMapLayerByUrl(url, false)`） | 第二个参数 `false`：不弹 HDF 子集对话框 |
| `memory:` 临时图层 | 会话 GeoPackage 文件经 geo 写出后加载；宿主内部临时数据用本地库 `tmp_` 前缀类 | 不依赖“内存图层随工程恢复” |
| 栅格图层 | `RasterDataSet.Open(path, ReadOnly)` + `RasterLayer.AttachData` | 栅格不入库，直接引用文件 |
| 图层组 | `GroupLayer` | M02 第 18 项 |
| CRS | `SRefData` | `Core/CrsMapper`；EPSG 号不是数据库 SrID，写入本地库前须先注册（第一版 `RouteDrawTool.cs:1613` 注释） |
| 动态投影 | 地图参照系 + 图层参照系 | M02 第 14 项；默认不改源数据坐标 |
| 单一符号 / 分类 / 分级渲染 | `SimpleTheme` / `UniqueTheme` / `RangeTheme`（图层 `Themes`） | M02 第 3 项；不写要素图形参数 |
| 规则渲染 | Core 表达式求值写分类字段 → `UniqueTheme` | 规则重叠时按第一条命中，并在回执中警告 |
| 多层符号 | 主线 + 辅助线型；表达不了的叠加层用“伴随显示图层” | 伴随图层不进 `get_layers_info` 主列表，在主图层下以 `companions` 列出 |
| QML 样式 | `*.style.yaml` + MapGIS 符号包 | `06` §1 |
| PAL 标注 | `LabelInfo` + `SimpleLabel`/`UniqueLabel` + `Point/Line/RegionPlaceInfo` + `TextAnnInfo` | 描边用 `TextAnnInfo.BackClr` 与 `BackExp`（第一版 `LayoutAddLabelTool.cs:330`） |
| 打印布局（可多个，具名） | `Document.GetLayout()`（单个） | M02 第 2 项；名称登记见 §5.5 |
| 地图项 | `MapElement`（`SetMap`、`SetViewRange`、`SetRange`、`GetGrid`/`SetGrid`） | 移动主图时同步平移视域（第一版 `LayoutSupport.ExpandElementToRect`） |
| 图例 | `LegendElement`（`GetLayerItem`、`SetGeometryDot`） | 按实际专题项生成条目 |
| 比例尺 | 自绘（第一版 `LayoutAddScalebarTool`） | 分母按主图实测比例尺 |
| 指北针 | `NorthArrowElement` + 样式库 | SVG 指北针先转图片 |
| 文字 | `TextElement`（`TextAnnInfo`、`Geometry = new Dot(x, y)`） | 追加后再次赋名 |
| 图片 | 图片元素（第一版 `RouteLayoutPictures.CreateElement`，PNG/BMP 已验证） | M02 第 10 项 |
| 形状 | `PolygonElement` / `LineElement` | — |
| 属性表 | 无 → 文本元素加线元素自绘 | 超出行数时截断，并在表下注明 |
| 格网 | `MapElement.GetGrid/SetGrid`（`GridInfo`） | 第一版已实现 |
| 导出 | `Layout.OutputToPDFFile(path, w, h, dpi, FileMode)`、`OutputToStream(w, h, type, …, out bytes)` | SVG 由 PDF 转换（geo，PyMuPDF） |
| QPT 模板 | 无 → `*.layout.yaml` | `06` §2 |
| 选择集 | 图层选择集 | M02 第 16 项 |
| Processing / PyQGIS | 无 → geo 运行时 | `04` |
| 色带（QgsStyle） | `Core/Symbology/ColorRamps`：ColorBrewer 全部色带，以及 QGIS 默认样式库中工具描述提到的色带（YlOrRd、Blues 等） | 未知色带名报错 |

## 4. 宿主适配层（P32）

### 4.1 `HostSession`

- `CurrentDocument()`：读 `IApplication.Document`。为空时返回 `host_not_ready`。
- `CurrentMap(create: bool)`：按 §3 规则返回当前地图。
- `ProjectPath()`：沿用第一版的路径稳定判定。文档正在打开或新建时，短暂的空路径不算关闭。
- 工具执行期间暂停工程状态采样，避免消息泵重入时把短暂的空路径误判为关闭（第一版 `DeferObservation`）。

### 4.2 `LayerRegistry`

- 负责分配、持久化、重新关联图层 ID（`01` §7.1）。
- 提供 `Resolve(ref)`：按 ID、唯一名或路径找图层；不唯一时报错并列出候选。
- 对外提供图层描述：ID、名称、类型、几何、要素数、坐标系、源 URL、可见性、所在地图、伴随图层。字段名与 QGIS 版 `get_layers_info` 一致（`autocarto/tools/system/_shared/project_state_impl.py:182` 起）。

### 4.3 `CrsMapper`

- `ToSRef(text)`：`EPSG:xxxx` 走 `SRefData.EPSGToSRS`，WKT 走 `WKTToSRS`，都失败就报错。
- `ToEpsg(SRefData)`：常用坐标系查对照表；查不到时输出 WKT。
- 对照表至少覆盖：EPSG:4326、4490（CGCS2000 地理）、3857、4534–4554（CGCS2000 3 度带）、4513–4533（CGCS2000 3 度带带号前缀）、326xx（UTM 北）、4214、4610。逐项在 M02 第 14 项实测往返。

### 4.4 `ColorLibrary` 与 `FontLibrary`

- **颜色**：RGB 转色号时，先在当前系统库颜色表中查完全相同的 RGB，没有就 `AddColor` 并 `Save`；同一进程内缓存（第一版 `LayerToolSupport.cs:654`）。
- **纯白**：纯白（色号 9）会被当作背景色（第一版 README §6），需要白色时用 RGB(254,254,254)，并在读回比较时视为白色。
- **透明度**：MapGIS 透明度为 0–100，0 表示不透明，与 QGIS 的 0–1 不透明度相反。换算统一在这里做。
- **字体**：按 `host.yaml` 的 `fonts.cjk_candidates` 依次查字体库。都没有时用库中第一个黑体或宋体，并在回执中给出警告。

### 4.5 `GdbWorkspace`

- 维持一个长期共享的本地库连接。第一版的做法是建立后不关闭，因为图层依赖这个连接。
- 管理导入（`DataConvert.OpenDestination(库 URL)`、`OpenSource(文件)`、`Convert()`）、类命名、同源判定（`01` §8）、临时类。
- **转换前**：先比对类名集合，再找新生成的类（第一版的做法），不猜宿主的派生命名规则。
- **同名类**：目标类重名可能触发原生崩溃（第一版注释），转换前必须检查。
- **只清空不删类**：删库中内容会触发原生访问冲突（第一版 `RouteDrawTool.cs:1489`），需要“替换”时一律清空后复用。

### 4.6 `SymbolPacks`

- 按资产文件 SHA-256 判断是否已安装（第一版 `LineSymbolLibrary` 的做法），只安装符号包里定义的符号。
- 符号包 XML 是 gb2312 编码，读写必须显式指定编码。
- 程序化生成虚线线型：周期恒为 10000 设计单位（10 毫米），厚度用几何表达，颜色写图元自己的 `InterColor`（第一版 README §6 的硬约束）。生成的符号命名为 `AutoCarto-dash-<段长列表>`，复用已存在的同名符号。

## 5. 各工具族实现要点

每个工具的语义以 QGIS 版处理器为准（下表“参照”列），返回结构逐字段对齐；差异写入 `tool-mapping.yaml` 的说明列，并同步到契约说明。

### 5.1 T40 工程状态（参照 `_shared/project_state_impl.py`）

| 工具 | 要点 |
|---|---|
| `open_project` | `document.Open(path)`，返回值为 1 才算成功（第一版 `OpenMapxTool.cs:39`）。打开后显示第一张地图，重新关联图层 ID，上报 `project_opened` |
| `create_project` | 当前文档未保存时，沿用其内容并 `SaveAs`；当前文档已有路径且与目标不同时拒绝，不换工程身份（第一版 `CreateMapxTool` 的规则：改路径会让会话对不上）；目标文件已存在时拒绝 |
| `save_project`（新增） | 只做原地保存；没有路径时报错，要求先用 `create_project`。保存前读取状态，保存后不立即回读原生文档（第一版注释：会触发崩溃）。另存为被外部改过路径时，带 `previous_path` 上报 |
| `close_instance` | M02 第 8 项；有退出接口就先保存提示再退出，没有就返回原因，不强杀进程 |
| `get_project_info`、`get_layers_info`、`get_active_layer_info`、`get_selected_features_info` | 字段与 QGIS 版一致；另带 `map_name`、`maps` |
| `add_map`、`get_map_info`（新增） | 第一版已实现，按新回执结构整理 |

### 5.2 T41 数据加载与图层管理（参照 `_shared/data_loading_impl.py`、`builtin_layers.py`）

- **`data_load_vector`**：
  - 文件经 `GdbWorkspace` 导入后挂图层；`gdbp://` URL 直接挂；`reuse_existing` 语义同 QGIS 版，按源指纹判断。
  - Shapefile 编码按 `.cpg`，缺省按 GBK；导入后抽查中文字段，有乱码时警告。
  - 文件声明的坐标系写入类的参照系；没有声明时报警告，不猜。
- **`data_load_raster`**：只读打开，不入库。
- **`data_load_list`**：按 items 底→顶顺序加载；`group` 写入图层组；`strict` 模式下必需数据缺失，就一层都不加载。
- **`layer_load_builtin`**：
  - 读内置图层清单 `layer.yaml`；需要裁剪时，由契约 `execution.prepare` 先调 geo 的 `builtin_clip` 生成派生副本，再交插件加载（`01` §6.4）。
  - 套默认样式（`style.yaml`），标注字段取清单的 `label_field`。
- **`layer_remove_created`**：按 ID 从地图移除，不删本地库中的类与文件。
- **`layer_export`（新增，系统内部）**：
  - 矢量优先用 `DataConvert` 把类导出为 GeoPackage（M02 第 15 项）。不支持时，逐记录读几何与属性，写成 GeoJSON（C# 自行序列化，坐标保留原参照系并写 `crs`）。
  - 返回路径、坐标系与 `data_revision`（类的记录数加最后修改时间的指纹）。
- **`layer_replace_source`（新增，系统内部）**：
  - 新文件入库为新类后，替换图层数据源；名称、ID、顺序、专题与标注保持不变。
  - 专题字段在新数据中缺失时，回执警告并保留原专题。
- **`data_load_tiles`**：M02 第 6 项。

### 5.3 T42 画布与视图（参照 `_shared/canvas_navigation_impl.py`）

- 通过第一版 `MapViewSync` 取得或创建 `"{地图名}$MapView"` 视图，再用 `MapControl` 设置比例尺、缩放到范围或图层、读取当前范围。M02 第 13 项确认接口名。
- 无界面或视图不可用时返回 `host_not_ready`，不影响数据操作。

### 5.4 T44 符号化与标注（参照 `_shared/symbology.py`、`style_basic_impl.py`、`style_thematic_impl.py`、`style_labeling.py`、`symbolize_mapping_impl.py`）

- **`layer_set_symbology`**：最关键，流程的底图画法全靠它。
  - **参数**：沿用 QGIS 版的 items 模式：`symbol.type` 为 fill/line/marker，`layers` 自下而上；`labels` 含字段、字号、颜色、字体、粗体、避让、放置、合并、重复、全部显示、描边；`scale` 统一缩放，线宽下限 0.1 毫米，字号下限 1.8 毫米。
  - **落地方式**：先用 `SimpleTheme`；第一层写主图形参数，第二层能用辅助线型表达就写辅助，否则建伴随显示图层；虚线用 `SymbolPacks` 生成的线型；点形状 circle/square/triangle/star 用符号包中的 `AutoCarto-marker-*`。
  - **读回**：回执同时给出“期望设置”（换算后）与“独立读回”（从图层专题和标注对象重新读取），字段结构与 QGIS 版一致，供 `flowrun/verify_symbology.go` 核对：颜色相同，线宽误差不超过 0.01 毫米。
- **专题渲染**：
  - `cartography_set_style`：单一或分类。
  - `cartography_graduated_style`：`RangeTheme`；断点在 Core 中计算，分位、自然断点（Fisher-Jenks）、等距、标准差的实现与 QGIS 结果对照；空值不参与分级。
  - `cartography_rule_based_style`：Core 表达式逐要素求值，结果写入隐藏分类字段 `ac_rule`（只写本地库副本），再建 `UniqueTheme`；规则重叠时按第一条命中，并警告。
- **标注**：
  - `cartography_add_labels` 和 `layer_set_symbology.labels` 共用同一套标注写入代码（第一版 `LayerToolSupport.ApplySimpleLabel` 的整理版）。
  - 放置：面标注为水平或沿骨架；线标注沿线，可去重或重复；点标注在点旁。
  - 描边用 `TextAnnInfo.BackClr` 与 `BackExp`。“全部显示”与“避让”按 M02 第 5 项确认的属性设置。
- **样式资源**：
  - `layer_load_style(s)`：解析 `style.yaml`，安装其引用的符号包，再按上面的规则写入。传入 QML 时返回 `unsupported_parameter`，提示“QML 仅用于 QGIS 版”。
  - `layer_save_style`、`layer_get_style_info`：把当前专题和标注写成 `style.yaml` 结构。
- **其他样式工具**：
  - `layer_set_opacity`：图层透明度，换算见 §4.4。
  - `layer_set_blend_mode`：按 M02 第 4 项实现，或降级。
  - `layer_set_color_ramp`、`layer_set_symbol_size_scale`：改写专题各项。
- **`cartography_symbolize_points_by_mapping`**：按映射表套点符号；符号名不存在就报错，不替换为其他符号（第一版 `ResolvePointSymbol` 的原则）。

### 5.5 T45 版面模型与元素（参照 `_shared/print_layout_impl.py`、`print_layout_items_impl.py`、`print_layout_advanced_impl.py`、`layout_runtime.py`）

- **单版面模型**（M02 第 2 项不通过时）：
  - 版面名称登记在文档旁的 `<工程名>.mapx.autocarto.json`，`layout_list` 最多返回一项；
  - `layout_create` 等于“清空版面、设置页面并登记名称”；`layout_delete` 等于“清空并注销”；`layout_rename` 只改登记名；
  - 对不存在的名称调用其他版面工具，返回 `invalid_params`。
- **坐标**：所有输入与输出的位置、尺寸都按 `01` §7.3 换算，对外保持 QGIS 约定。
- **元素工具**：
  - 地图框、文字、图例、比例尺、指北针、格网、花边、图片、形状、概览图、属性表：语义同 QGIS 版；第一版已有的实现去掉写死的颜色和条目。
  - 查询、移动、缩放、对齐、分布、层级、分组、锁定、旋转、删除：语义同 QGIS 版。
- **主图视域**：
  - 移动或缩放主图元素时，视域与矩形同步变化；返回 `view_covers_data`，用于发现内容跑到框外（第一版 `LayoutSupport.cs:595` 的经验）。
  - `layout_set_map_layers` 为版面建立只含所选图层的专用地图，共享数据，不复制。
- **警告规则**：越界与前景元素重叠沿用 QGIS 版（v7 D31 版面警告），由 Core 计算。

### 5.6 T46 版式合成、模板与导出（参照 `_shared/layout_compose_impl.py`、`layout_export_impl.py`、`layout_template_save_impl.py`）

- **`layout_compose`**：
  - 把 `layout_compose_impl.py` 的版式逻辑移植到 `Core/Layout`：纸张 A0–A4 或宽×高、方向、按纸张选字号、元素锚点（`anchor.to/at/placement/offset_mm/scale_offset_with_page`）、主图填充内容区、图例白名单与显示名、额外图例条目、综述、元素开关、返回遮挡与越界警告。
  - Core 只算几何，插件按结果创建元素。
  - 两个内置版式（四角对齐、统计专题）必须逐元素与 QGIS 版位置一致，容差 0.5 毫米；由 V73 对照。
- **导出**：
  - `export_layout_pdf`、`export_print_layout`：PDF 用 `OutputToPDFFile`，PNG/JPG 用 `OutputToStream`；SVG 先出 PDF，再交 geo 转换。
  - `layout_export_image`：DPI 与尺寸参数同 QGIS 版，并校验实际编码（第一版 `LayoutImageExportSupport` 的做法）。
- **快照**：`layout_get_items_properties` 写出版面快照，路径放在 `data.layout_snapshot_path`，结构逐字段与 QGIS 版一致（质量审查脚本直接读取）。
- **模板**：`import_layout_template` 只收 `.layout.yaml`；`layout_save_template` 把当前版面写成 `.layout.yaml`，主图元素名为 `main_map`。

### 5.7 T47 图文标注（参照 `_shared/callouts_impl.py`、`print_layout_callout_impl.py`、`callout_text_layout.py`）

- **读取**：数据集由前置处理解析（`01` §6.4）；标注 CSV 结构同 QGIS 版。
- **创建**：建图文框（底框、正文、可选图片、引线），按 `avoid_item_ids` 避让。返回分组与实际元素 ID。
- **排版**：文字换行与字号测量用 `System.Drawing` 按实际字体测量。复用第一版的排版与图片处理，去掉写死的框宽和字号，改读参数与版式。

### 5.8 T48 行军路线绘制（参照 `_shared/route_drawing_impl.py`、`route_points_impl.py`，内核用第一版 C#）

- **`route_draw`**：
  - 参数与回执同 QGIS 版：`dataset_source`、`dataset_dir`、`expected_table_counts`、`expected_task_id`、`route_style`、`label_layer_name`、`strict`；`renderer_plugin_dir` 在 MapGIS 版不使用，传入时忽略并在回执中说明。
  - 数据集交接与核验通过契约 `execution.prepare` 交给 geo 的 `route_dataset_resolve`（复用 `autocarto.knowledge.dataset_handoff`），结果在 `dataset_resolved` 参数中（`01` §6.4）。插件只做几何与落图。
  - 几何由 `Core/Route` 计算（第一版贝塞尔、短线限宽、分叉与汇入规则）；Flox 后端为可选（需要 Java 17+，用 `AUTOCARTO_FLOX_JAVA` 指定），不可用时保留贝塞尔基线并说明原因。
  - 样式读取 `route-cartography` 的样式资源（红蓝对比、单色），不写死颜色。
  - 每条路线返回回执，含实际图层名与 ID。
  - **只画路线与名称标注**。专题点、版面、图文框分别由 `route_points_build`、`layout_compose`、`layout_add_callouts` 完成；第一版把它们合在一个工具里。
- **`route_points_build`**：`points.csv` 加 `symbol_mapping.csv`，套军标符号包中的点符号，保留地名与会师参与者标注。

### 5.9 T49 资源预览（参照 `_shared/resource_preview_impl.py`）

- M02 第 7 项通过时：新建离屏 `Document`，加入示例数据或指定图层，套样式或版式，`OutputToStream` 出 PNG，然后释放。全程不触碰宿主当前文档。
- 不通过时：工具改由 geo 生成示意图（matplotlib 按 `style.yaml` 绘制），在资源页标注“示意”。

## 6. M02 能力探查清单

每项写一个最小探查程序，放在 `plugin/AutoCarto.MapGIS.Plugin/Probes/`，用调试工具 `autocarto_probe`（只在环境变量 `AUTOCARTO_PROBES=1` 时注册）在真机运行。结论写入 `evidence/M02-probes.md`：现象、代码片段、结论、对 `host.yaml` 能力项的影响。

| # | 问题 | 为什么重要 | 影响 | 不通过时 |
|---|---|---|---|---|
| 1 | 图层有没有可随 .mapx 保存的自定义属性 | 图层 ID 持久化 | P32、T40 | 工程旁路文件 |
| 2 | 一个文档能否有多个版面 | 版面工具语义 | T45 | 单版面加名称登记 |
| 3 | `SimpleTheme`、`UniqueTheme`、`RangeTheme` 能否设置、保存、在两种导出中正确显示；与 `SymbolShow` 的关系 | 样式不改数据 | T44 | 在会话副本类上写要素图形参数 |
| 4 | 图层混合模式 | `layer_set_blend_mode` | T44 | 只支持 normal |
| 5 | 标注：描边、背景、沿线、重复间隔、避让、全部显示、同名线合并 | 流程画法 | T44 | 不支持的项回执警告 |
| 6 | 在线瓦片、WMTS、天地图图层 | `data_load_tiles` | T41 | 不提供 |
| 7 | 离屏 `Document` 渲染是否影响宿主文档 | 资源预览 | T49 | geo 示意图 |
| 8 | 宿主退出接口 | `close_instance` | T40 | 返回原因 |
| 9 | 版面元素旋转、锁定、分组 | 版面工具 | T45 | 插件登记锁定和分组；旋转只支持文字 |
| 10 | 图片元素支持的格式（PNG、JPG、BMP、EMF、SVG） | 图片、图表、指北针 | T45、T46、T47 | 其他格式先转 PNG |
| 11 | PDF 导出：矢量还是栅格、字体是否嵌入、尺寸 | 成果质量 | T46 | 记录限制 |
| 12 | 栅格显示：灰度拉伸、伪彩色、透明度、无数据值 | 山体阴影、DEM 等栅格 | T41、T44 | 只用灰度并给出说明 |
| 13 | `MapControl` 缩放、比例尺、范围接口 | 画布工具 | T42 | — |
| 14 | `SRefData` 与 EPSG/WKT 往返（§4.3 对照表逐项）；动态投影是否默认生效 | 坐标正确性 | P32、T41 | 无法映射的坐标系报错 |
| 15 | `DataConvert` 能否把本地库类导出为 GeoPackage、GeoJSON、Shapefile；编码 | `layer_export` | T41 | C# 逐记录写 GeoJSON |
| 16 | 选择集读写与刷新 | 选择工具 | T43 | — |
| 17 | 添加字段、按 OID 写属性、写后读回；`SaveEdit` 的返回值含义 | 规则分类字段、数据替换 | T41、T44 | — |
| 18 | `GroupLayer` 的创建与加入 | 图层组 | T41 | 不分组，并在回执中说明 |
| 19 | 程序化导入符号 XML、生成虚线线型；点符号能否引用图片 | 样式与军标 | T44、R61、R63 | 预先制作符号包 |

M02 还要完成两件事：

- 用 `tools/MapGisApiDump` 导出 `MapGIS.*.dll` 的全部公开类型与成员，写到 `evidence/mapgis-api/<程序集>.md`；
- 记录本机 SDK 帮助文件与开发包的位置和版本（例如开发帮助 CHM、司马云下载的 .NET 开发包）。

## 7. 编码规范与禁忌

1. 一个工具一个文件，类为 `internal static`，入口方法带 `[HostTool("工具名")]`，返回 `ToolResult`。工具名与契约 `execution.backend_tool` 一致，V70 检查。
2. 访问 MapGIS 对象只在 UI 线程（工具入口已在 UI 线程），且每个原生调用都经 `NativeGuard.Try`。不要在 `Task.Run` 或计时器线程里访问文档、地图、图层。
3. **不写场景常量**：纸张、颜色方案、图例条目、字段值到符号的映射、写死字号，都只能来自参数、版式或样式资源。Core 中的通用默认值（例如线宽下限 0.1 毫米）集中放在 `Defaults.cs`。V70 用规则扫描 `Handlers/`。
4. 不弹任何模态对话框：凡有“是否弹窗”参数，一律传 `false`。宿主若弹出对话框，工具会一直悬挂。
5. 不对宿主文档 `Close`、`Dispose`、`New`。只在 T40 的工程工具中按明确语义 `Open`、`Save`、`SaveAs`。
6. 不删除本地库中的类（会触发访问冲突），只清空后复用。
7. 回执的人可读结论放在 `output`，结构化事实放在 `info`/`data`，不在 `output` 里复述清单（第一版 README §3.8 的约定）。
8. 日志不写出密钥与用户数据内容，只写工具名、参数键、耗时与结论。
9. 宿主运行时，不要在插件输出目录里改文件：DLL 会被锁定，`dotnet build` 报 MSB3021/MSB3027。构建前先关闭 MapGIS（W22 的构建脚本会检测）。

## 8. 测试

| 层 | 内容 | 位置 | 运行环境 |
|---|---|---|---|
| Core 单元测试 | 协议、队列、表达式（共享用例）、版式几何、坐标换算、分级断点、色带、图层 ID 重新关联、路线几何 | `AutoCarto.MapGIS.Core.Tests` | Linux 或 Windows，`dotnet test` |
| 契约样例 | 每个宿主工具一组请求和期望回执结构（JSON Schema），从 QGIS 版处理器的实际回执提取 | `autocarto_mapgis/spec/contracts/<工具>.json` | 由 P33 的真机联调脚本使用 |
| 真机联调 | `scripts/mapgis_host_harness.py`：扮演后端，写 `.port`，接受插件连接，按场景 YAML 依次下发 `tool_invoke` 并校验回执 | `scripts/` | Windows + MapGIS |
| 固定场景 | 不经模型，固定角色执行 5 个场景，导出 PDF/PNG，并读回符号与版面 | `autocarto_mapgis/tests/scenarios/` | Windows + MapGIS |

测试使用独立运行目录与独立本地库（`AutoCarto_test`），不写用户的 `AutoCarto` 库。
