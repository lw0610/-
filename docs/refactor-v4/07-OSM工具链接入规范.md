# 07 OSM 工具链接入规范

本文件规定如何把 GeoCode 的 OpenStreetMap（OSM）专用下载工具链移植进 AutoCarto，既供智能体在对话中直接使用，也作为数据获取子系统 `overpass` 适配器的底层（`03` §5.5）。

## 1. 为什么要接入

| 现有能力 | 能拿到的数据 | 拿矢量底图数据（道路、铁路、河流、水域、站点、地名点、建筑）时的问题 |
|---|---|---|
| 浏览器自动化（`browser_*`，已启用） | 任何网页上能操作出来的东西 | 每一步都靠模型操作网页，慢、容易中断、结果不可复现；适合门户网站兜底，不适合批量矢量数据 |
| 网页取数（`web_read_page`、`web_download_file`，已启用） | 已知地址的公开文件 | 只支持 GET；必须先知道文件地址；拿到的 OSM 原始数据没有几何组装 |
| GEE（`gee_*`，已启用） | DEM、影像、地表覆盖、夜光、人口等栅格；少量矢量（如无名称的河网） | 没有 OSM 道路、铁路、地名；矢量导出受体量限制 |
| 内置离线行政区边界 | 省、市、县行政区面 | 只有行政区 |

结论：现有三项扩展服务能覆盖栅格和门户网站数据，但**没有一条可靠、确定性的矢量底图数据通道**。GeoCode 的 OSM 工具链正好补这一块，且已经在国内网络环境下做过实测和加固。

## 2. 上游

| 项目 | 内容 |
|---|---|
| 来源 | GeoCode-Release 发行包 `GeoCode-source-v0.9.6.zip`（2026-06-25） |
| 许可 | MIT（GeoCode；其上游 OpenCode 同为 MIT，见发行包 `LICENSE`、仓库 `NOTICE`） |
| 主要文件 | `packages/opencode/src/tool/geo/osm-search.ts`（305 行）、`osm-probe.ts`（231）、`osm-get-objects.ts`（278）、`osm-get-features.ts`（295）及同名 `.txt` 说明；`osm/client.ts`（327）、`query.ts`（235）、`extent.ts`（258）、`convert.ts`（278）、`centroid.ts`（122）、`output.ts`（59）；子智能体提示 `agent/prompt/osm-download.txt`；测试 `test/tool/osm-query.test.ts`、`osm-extent.test.ts`、`osm-convert.test.ts`（共 758 行） |
| 第三方依赖 | `osm2geojson-lite` 2.0.1（几何组装；许可需核实） |
| 本仓库先例 | GEE 技能与内置行政区边界已按同一方式从 GeoCode v0.9.6 移植，来源记录见 `autocarto/skills/system/gee-remote-sensing/UPSTREAM.md`、`autocarto/tools/system/admin-boundaries/assets/中国行政区边界/UPSTREAM.md` |

移植要求：新工具域写 `UPSTREAM.md`，固定发行包版本、列出参考文件与原文 SHA-256、说明本地修改；保留 MIT 版权声明（GeoCode 与 OpenCode）。只移植行为与测试用例，不复制 TypeScript 运行时。

## 3. 要继承的行为契约

### 3.1 网络客户端（对国内网络尤其重要）

| 契约 | 上游做法（`osm/client.ts`） |
|---|---|
| 只用全量数据节点 | 8 个全球全量 Overpass 节点；区域节点（只含某国数据）不得加入，否则会静默返回不完整结果 |
| 首次测速排序 | 首次调用时并行发送极小查询（8 秒超时），按响应速度排序，连不上的排到最后——上游注释写明国内网络常连不上德国节点但能连其他节点，排序后零配置可用 |
| 固定健康节点、逐个切换 | 成功的节点下次优先；失败按顺序换下一个；一轮全部失败后按 2、4、8 秒退避，最多 3 轮；全部失败后清空排序，下次重新测速 |
| 全局节流 | Overpass 与 Nominatim 各自两次请求间隔不少于 1.1 秒（遵守公共服务使用规则） |
| 流式体量上限 | Overpass 以分块传输返回、没有长度头，必须边读边计数，超过 200 MB 立即中止并提示“缩小范围” |
| 软失败识别 | 返回体中的 `remark`（如查询超时）视为该范围失败；HTTP 200 但返回 HTML 错误页视为失败 |
| 请求方式 | `POST`，`data=<查询>`；带 `User-Agent` |
| 对象回退 | 按编号取对象时，Overpass 取不到的逐个回退到 OSM API `/full.json`；大关系组装后顶点数不足原始节点数一半时拒绝输出，防止“看似成功的残缺边界” |

### 3.2 范围（`osm/extent.ts`）

- 四种写法：`area:R<关系编号>`（只能用关系）、`bbox:南,西,北,东`、`around:半径米,纬度,经度`、`file:<边界 GeoJSON 路径>`（必须是 WGS84，多面全部查询）。
- **只多不漏**：边界面顶点不超过 300 时精确使用；超过时用凸包代替；部件超过 8 个时用整体外包框。凡是近似的，结果标 `clipped=false`，后续必须精确裁剪。

### 3.3 查询（`osm/query.ts`）

- 标签写法：`键=值`、`键`（存在即可）、`键~正则`，以及原样传入的方括号写法；值做转义。
- 每个范围 × 要素类型 × 标签生成一个并集子句；输出 `out geom`（几何随要素一并返回）。
- 名称搜索：关系、线、点分别设上限且关系优先，避免全球同名的点挤掉边界候选；只有包含拉丁字母时才大小写不敏感（中文查询不付这个开销）。

### 3.4 转换与输出（`osm/convert.ts`、`centroid.ts`、`output.ts`）

| 契约 | 内容 |
|---|---|
| 逐要素转换 | `out geom` 结果逐个转换，一个要素组装失败不会连带吞掉其他要素；组装失败的要素编号单独报告 |
| 字段约定 | 每个要素固定有 `osm_id`、`osm_type`、`name`、`category`（如 `highway=primary`），加上查询用到的标签键和 `fields` 指定的键；其余标签丢弃但按键计数报告；`fields: ["*"]` 时把全部标签无损保存在 `osm_tags` 列 |
| 去重 | 多个范围合并时按 `osm_type/osm_id` 去重 |
| 点模式 | `geometry=centroid` 时所有要素转为代表点，原几何类型记在 `original_geometry`；做兴趣点数据时不能只取 `node`，否则面状设施会丢失 |
| 不写混合几何文件 | 结果含多种几何时拆成 `-points`、`-lines`、`-polygons` 三个文件；重新下载时清理上次遗留的同名拆分文件 |
| 不改几何 | 坐标系 EPSG:4326，不简化、不缓冲、不合并（取舍与简化在我方规范化步骤做，`03` §6） |
| 部分结果明示 | 有范围失败、要素组装失败或被取消时，输出以“部分结果”开头，并列出失败范围与丢失要素 |

### 3.5 使用方法（`agent/prompt/osm-download.txt`）

先搜索确定对象 → 选定编号 → 批量下载前先统计数量（超过约 3 万要素就按下级行政区拆分范围，一次最多 12 个范围）→ 下载 → 读取文件核验 → 报告实际路径、数量、坐标系、保留与丢弃的字段、缺失部分。另有一份按类别整理的标签并集清单（医疗、餐饮、教育、商业、公交站点、公园绿地、道路、建筑）。这些内容改写成中文工具包说明（§5）。

## 4. 在 AutoCarto 中的实现

### 4.1 位置与结构

| 部分 | 位置 | 说明 |
|---|---|---|
| Go 运行时 | 新建 `carto_agent/internal/osmdata/`（`client.go`、`query.go`、`extent.go`、`convert.go`、`centroid.go`、`output.go`、`tools.go`） | 与 `internal/webdata`、`internal/geeruntime` 同级；由 `internal/integrations/tools.go` 分发 |
| 工具定义 | 新建 `autocarto/tools/system/osm-data/*.yaml`（`runtime: internal`） | 见 §4.3 |
| 工具包 | 新建 `autocarto/skills/system/osm-data/SKILL.md` | 显示名“开放地图数据（OSM）”，领域“在线数据” |
| 来源记录 | `autocarto/tools/system/osm-data/UPSTREAM.md`、`LICENSE` | §2 移植要求 |
| 文件信息工具 | 新建 `geodata_inspect`（QGIS 端，F61） | 对应上游 ReadGeoData：不加载到工程即可读取坐标系、范围、要素数、字段与样本，供核验与文件区使用 |

### 4.2 几何组装的实现方式（F60 先做技术验证）

两种方式，以“通过移植过来的上游转换测试用例”为准选择，结论写入验证记录：

1. 使用 Go 库 `github.com/paulmach/osm` 及其 `osmgeojson`（MIT）——需验证它能否正确处理 Overpass `out geom` 结果，或改用“全量递归”查询后仍满足 §3.4 的逐要素契约；
2. 移植 `osm2geojson-lite` 中需要的逻辑（线段拼接、多面外环内环组装、闭合线判面规则）——许可核实后保留其版权声明。

无论哪种，§3.4 的契约与上游测试用例都必须通过。

### 4.3 工具

| 工具 | 显示名 | 参数（中文说明写进 YAML） | 返回 |
|---|---|---|---|
| `osm_check_environment` | 检查 OSM 服务 | 无 | 各节点连通性与延迟排序、Nominatim 是否可达、当前代理设置 |
| `osm_search` | 查找 OSM 对象 | `query`、`context`（地区上下文）、`category`（标签提示） | 候选：编号（R/W/N 前缀）、类别、行政级别、中心点、来源（两种引擎都命中为最高可信）、是否公交噪声、站点关系成员 |
| `osm_probe` | 统计 OSM 要素 | `extents`、`tags`（计数模式）或 `key`（分布模式）、`types` | 每个范围的点/线/关系数量及合计；或某标签键的取值分布 |
| `osm_get_objects` | 下载 OSM 对象 | `ids`（建议不超过 10 个）、`fields`、`output` | 文件路径（可能拆分）、要素数、几何统计、保留/丢弃字段、缺失对象及原因 |
| `osm_get_features` | 按范围下载 OSM 要素 | `extents`（最多 12 个）、`tags`、`types`、`geometry`（`original`/`centroid`）、`fields`、`output`、`timeout`（默认 120 秒） | 同上，另有失败范围、组装失败要素、`clipped`、`partial` |

我方调整：

| 项目 | 上游 | AutoCarto |
|---|---|---|
| 输出位置 | 必须给绝对路径 | `output` 可省略或只给文件名，默认写入当前会话 `output/osm/`；绝对路径只能位于本会话目录或当前工程目录 |
| 覆盖 | 同一路径直接覆盖 | 默认不覆盖：同名时自动加序号；只有 `overwrite=true` 且目标在本会话 `output/osm/` 内才覆盖（与网页取数“默认不覆盖”的规则一致） |
| 来源记录 | 无 | 每个输出旁写 `<文件名>.source.json`：查询语句、范围、标签、使用的节点、获取时间、许可“ODbL 1.0”、署名“© OpenStreetMap contributors”、数量、保留与丢弃字段、是否部分结果 |
| 核验工具 | ReadGeoData | `geodata_inspect`（F61） |
| 中文名称 | 需要时自行在 `fields` 里加 `name:zh` | 说明中写明：中文制图默认加 `fields: ["name:zh"]`；获取子系统的字段映射按 `name:zh` → `name` 取值 |
| 下载前确认 | 每次下载前询问用户 | 同一会话首次使用 OSM 下载时确认一次（复用 `hitl_request`）；流程中由用户选择了“自动获取”的，不再询问（README §8 第 18 项） |
| 疆域提示 | 无 | 下载 `boundary=administrative` 且范围与中国相交时，结果附提示“OSM 行政边界不代表我国标准地图，制图请使用内置行政区边界”；获取规划中疆域类条目本来就排除 OSM（`02` §5） |
| 产出登记 | — | 输出文件登记到会话文件索引（`08` §5），来源标“OSM 下载” |

### 4.4 配置（`config.yaml` → `integrations.osm`）

```yaml
integrations:
  osm:
    enabled: false            # 在“设置 → 扩展服务”开启
    endpoints: []             # 空表示使用内置的全量节点清单；自定义时只能填全量节点
    nominatim_url: ""         # 空表示官方 Nominatim
    proxy_url: ""             # 与 GEE 相同的无密码 HTTP(S) 代理写法
    timeout_seconds: 120
    max_response_mb: 200
```

- `internal/config/integrations.go` 增加 `OSMConfig`，`Validate` 校验地址、代理与范围；设置界面“扩展服务”增加一行（F60 同时修改前端设置页中扩展服务部分）。
- 数据源目录中 OSM 来源的前置条件改为 `integration:osm`（`02` §4.4）。

### 4.5 与数据获取子系统的关系

- `03` §5.5 的 `overpass` 适配器直接调用 `internal/osmdata` 的客户端、范围、查询与转换，不经过模型；类别到标签的映射写在数据源目录 `source.osm.overpass` 的 `provides[].query` 中（改为标签并集写法，与 `osm_get_features` 一致）。
- 获取范围来自 `02` §3.4；`clipped=false` 时由 `data_normalize` 精确裁剪。
- 全国或超大范围由规划器改选 OSM 中国区域提取包（`source.osm.geofabrik-china`，`http_file` + GDAL 读取），这是上游没有的，用于补足在线查询的体量上限。

### 4.6 谁能用

- 默认方案的“数据工程专家”增加 `osm-data` 工具包（README §8 第 16 项）。不改用户已有方案；有了 `01` 的临时补齐，流程可以按需申请这个工具包。
- 上游把 OSM 下载放在一个专用子智能体里，目的是隔离上下文、限制联网范围。AutoCarto 的委派本来就在独立的子智能体上下文中执行，工具权限由工具包决定，因此不必新增角色；需要时用户可以在方案中自建一个只含 `osm-data`、`data-loading` 的角色。

## 5. 工具包说明（`osm-data/SKILL.md` 要点）

按上游使用方法改写为中文，至少包含：

1. 流程：查找 → 选定编号 → 统计数量 → 下载 → 用 `geodata_inspect` 核验 → 报告。
2. 选定规则：边界优先关系，不用地名点；两种引擎都命中的最可信；公交噪声通常排除；多校区等多部件实体全选；同名地区按上下文区分，确实无法判断时再问用户。
3. 模式选择：按名称取对象用 `osm_get_objects`；“某范围内所有某类要素”用 `osm_get_features`；超过约 10 个编号就改用按范围下载。
4. 标签并集清单（中文类别 → 标签）：道路、铁路、地铁与轻轨、公交与轨道站点、河流、水域、医院诊所、学校、商业、公园绿地、建筑、行政边界。
5. 兴趣点与区域：兴趣点用 `geometry=centroid`，不要只取 `node`；区域用 `types=way,relation`。
6. 体量：先 `osm_probe`；超过约 3 万要素按下级行政区拆分范围。
7. 字段：下载前按用途选 `fields`（道路：等级、编号、车道；中文制图：`name:zh`）；报告保留与丢弃的字段。
8. 部分结果必须如实报告；行政边界的疆域提示必须转述给用户；成果署名“© OpenStreetMap contributors”。

## 6. 测试

- 把上游三份测试（查询构造、范围、转换，共 758 行）改写为 Go 表驱动测试，覆盖转义、凸包超集、多部件、逐要素转换不互相吞并、字段约定、去重、点模式、拆分与遗留文件清理。
- 客户端用本地假服务器测试：测速排序、固定节点、逐个切换、三轮退避、1.1 秒节流、流式超限中止、`remark` 与 HTML 错误页识别、OSM API 回退与残缺边界拒绝。
- 工具层测试：默认输出位置、不覆盖、路径越界拒绝、来源记录内容、疆域提示、产出登记。
- 真实联网测试仅在 `AUTOCARTO_LIVE_ACQUIRE=1` 时运行：在一个小范围下载道路并核验；记录各节点在当前网络下的可达性。

## 7. 风险

| 风险 | 处理 |
|---|---|
| 公共 Overpass 节点不稳定或限流 | 测速排序、切换、退避、节流；大范围改用区域提取包；结果写入数据仓复用 |
| OSM 中国区域数据完整性参差 | 核验要素数与名称填充率（`03` §7）；计划中注明“开放数据，完整性因地区而异” |
| 中国疆域表达 | 疆域类条目不使用 OSM；下载行政边界时附提示 |
| 许可 | ODbL 要求署名与相同方式共享衍生数据库；来源记录与「数据来源说明」写明；分发衍生数据集前提醒用户 |
