# 在线取数与 Google Earth Engine 接入

AutoCarto 现已提供三条接入路径：公开网页与文件下载、独立浏览器操作、独立 Python 环境中的 Google Earth Engine（GEE）处理。它们通过现有内部工具体系调用，输出可交给数据处理工具检查后加载到 QGIS。

实际能否完成任务，以本机环境检查、真实工具返回和完整生成文件为准。环境检查通过、脚本正常退出、影像完整下载、云任务提交和云端导出完成是不同状态；代码或单元测试通过不代表任意 Google 账号、地区、日期或数据集都已完成端到端验证。

## 用户设置

打开“系统设置 → 在线数据与浏览器”。GEE、浏览器、网页读取与下载分别控制，默认全部关闭。修改后可点击本卡片的“保存在线数据配置”，也可直接点击“保存并检查 GEE 运行环境”或“保存并检查浏览器运行环境”：它会先保存本卡片的全部修改，再检查所选环境。保存失败会保留输入、显示错误原因，并跳过检查；修正错误后可以重试。打开设置页不会自动修改用户配置或启动外部命令。

保存后，即使服务仍关闭，也可以手动点击“检查 GEE 运行环境”或“检查浏览器运行环境”排查依赖。GEE 检查会核对选定 Python 的 Earth Engine 和 geedim 依赖，并使用已有凭据初始化；不会安装包或发起账号授权。浏览器环境检查只检查本地启动程序是否存在，不打开网页，不证明网络、MCP 服务或 Chrome 已能运行；真正的浏览器任务才会尝试启动浏览器并访问目标网站。

GEE 的连接与下载分开判断：`environment.ready=true` 只表示 Earth Engine 已能初始化、可以查询；本地影像下载还要求 `environment.download_ready=true`。缺少或版本不兼容的 geedim 会返回 `ready_without_local_download`，设置页显示“可查询；本地影像下载尚未就绪”，并列出 `download_dependency_error` 和所需版本。请在配置的独立虚拟环境补齐依赖后重查，不能将这类结果视作完整就绪。

如果页面提示“当前后台服务尚未更新”，保存和检查会暂时禁用。请重新启动更新后的桌面应用，让它加载新版本后台；仅刷新页面不会替换仍在运行的旧后台。

### 配置 GEE

建议使用单独的 Python 虚拟环境，避免修改 QGIS 自带 Python。下列命令由用户在终端手动执行；目录可换成自己的位置。

macOS / Linux：

```sh
python3 -m venv ~/.venvs/autocarto-gee
~/.venvs/autocarto-gee/bin/python -m pip install earthengine-api geedim==2.0.0 geopandas==1.1.1
~/.venvs/autocarto-gee/bin/python -c "import ee; ee.Authenticate()"
```

Windows PowerShell：

```powershell
py -m venv "$env:USERPROFILE\.venvs\autocarto-gee"
& "$env:USERPROFILE\.venvs\autocarto-gee\Scripts\python.exe" -m pip install earthengine-api geedim==2.0.0 geopandas==1.1.1
& "$env:USERPROFILE\.venvs\autocarto-gee\Scripts\python.exe" -c "import ee; ee.Authenticate()"
```

在“Python 解释器”中填写该虚拟环境解释器的**绝对路径**。在浏览器中自行完成 Google 授权，然后填写已注册 Earth Engine、已启用对应 API、且当前账号有权限使用的 Google Cloud 项目 ID。凭据由 Earth Engine 自己管理，不写入 AutoCarto 的 integrations 配置。认证和项目准备步骤以 [Google 官方认证指南](https://developers.google.com/earth-engine/guides/auth) 为准。

`earthengine-api` 提供官方 Earth Engine API；固定的 `geedim==2.0.0` 负责把大图分为符合单请求限制的小块，再拼成完整本地 GeoTIFF；`geopandas==1.1.1` 及其依赖负责本地边界读取、投影转换和几何验证（这里使用已实测版本）。无需额外安装 geemap，也不要把这些包安装进全局或 QGIS 自带 Python。[geedim 官方项目](https://github.com/leftfield-geospatial/geedim)

需要代理时填写 GEE 专用 HTTP(S) 代理地址，例如 `http://127.0.0.1:7890`；不接受包含账号密码、查询参数或片段的 URL。该配置只传给 GEE 进程，不能视为网页下载或浏览器的全局代理。脚本无输出超时默认 300 秒、最多 3600 秒；进度和阶段心跳会延长等待，与 GeoCode 的空闲超时方式一致。下载器自动分块并定期输出心跳，不必为了总时限自行拆成南、中、北多次下载。环境检查、任务查询和取消操作仍有单次总时限。可以先保存空项目 ID 排查依赖，但实际 GEE 初始化仍需要有效项目。

### 配置浏览器

默认方式需要 Node.js 18+、`npx` 及已安装的 Google Chrome；也可在设置中选择 Edge 或 Chromium，但须自行准备相应浏览器。Node 要求来自 [Microsoft 的 Playwright MCP 官方项目](https://github.com/microsoft/playwright-mcp#requirements)。本项目固定使用 `@playwright/mcp@0.0.83`，不自动追随 `latest`。

```text
启动命令：npx
启动参数（每行一个）：
-y
@playwright/mcp@0.0.83
```

可以手动执行 `npx -y @playwright/mcp@0.0.83 --help` 检查服务命令；首次运行可能下载 npm 包。AutoCarto 的环境检查本身不会执行该命令，真正调用浏览器工具时才会启动服务。若桌面应用找不到终端中的命令，可填写命令绝对路径；自定义 Node 启动方式还需要正确的 MCP `cli.js` 参数。

每个聊天使用独立浏览器会话；不继承日常 Chrome 的 Cookie、登录状态或已有标签页。登录、验证码和账号授权须由用户在弹出的浏览器中完成，此时应关闭 Headless。关闭浏览器后该会话的登录状态不会保留。该行为对应 [Microsoft 官方的 isolated 模式](https://github.com/microsoft/playwright-mcp#user-profile)。独立进程/会话不等于操作系统安全沙箱。

### 配置公开网页读取与下载

启用“网页读取与文件下载”后，可直接读取公开 HTTP(S) 页面，或下载已知链接的文件。默认请求超时 60 秒、单文件上限 100 MB；设置允许 1–1024 MB。实现按 `值 × 1024 × 1024` 字节计算，即 MiB。

`web_read_page` 不执行网页脚本，不共享浏览器登录。动态表单、登录后的页面和需要点击的下载入口应使用浏览器。网页响应最多 4 MiB，返回正文默认最多 20,000 字符，可用 `max_chars` 设置为 1,000–100,000；截断会明确标记。只访问公开网络地址，拒绝本机和内网地址。

`web_download_file` 使用单次请求限时和流式大小检查；超限或传输失败不会返回成功文件，未完成目录会被清理。它不会自动解压、重投影或判断科学有效性。网页下载上限只控制这一工具，不会改写 GEE 的直接下载限制，也不是浏览器下载大小的统一限制。

## 两条可直接使用的对话示例

以下示例需要相应服务已配置并启用。这里只给出请求样例，没有宣称已运行成功。

**GEE 小范围 DEM 下载：**

> 检查 GEE 环境。使用 USGS/SRTMGL1_003 的 elevation 波段，下载经纬度矩形 [116.35, 39.90, 116.36, 39.91] 的 DEM，输出分辨率 30 米，CRS 为 EPSG:4326，保存为当前会话的 dem.tif。返回真实文件路径、数据源和实际范围，检查栅格后加载到当前 QGIS 工程。不要把 SRTM 数据说成当前年份观测。

数据集和波段可在 [Google 官方 SRTM 目录](https://developers.google.com/earth-engine/datasets/catalog/USGS_SRTMGL1_003) 核对。该数据的观测时间为 2000 年，不能从下载时间推断为新观测。

**浏览器发现下载入口，再下载原始文件：**

> 使用独立浏览器打开 Natural Earth 的 1:110m Land 页面（https://www.naturalearthdata.com/downloads/110m-physical-vectors/110m-land/），读取页面上的真实下载链接。使用网页下载工具把原始 ZIP 保存到当前会话，返回文件路径、来源 URL、下载时间和 SHA-256；保留原始 ZIP，检查其中的矢量数据后加载到 QGIS。遇到登录或验证码停下来让我处理。

该入口来自 [Natural Earth 官方 Land 页面](https://www.naturalearthdata.com/downloads/110m-physical-vectors/110m-land/)。运行时重新读取链接，避免把曾经有效的地址写死。

## GEE 直接下载与云端导出

GEE 按 GeoCode 的“模型读取任务和技能 → 生成脚本 → 检索处理 → 检查覆盖 → 下载核验”流程执行。模型结合任务与可用数据选择数据源、时段、波段和处理方法；没有固定的“最近90天”窗口，也不要求用户机械填写卫星、日期、用途三项。已有明确条件优先，只有影响目标含义的缺失信息才需要澄清。原始影像检索下载交给数据工程专家，合成、指数等分析交给空间分析专家，两者均分配 GEE 能力包。

本地工作流程技能 `gee-scripting` 包含完整的影像合成、分类、变化检测参考，以及 `templates/single-date-image.md` 和可调用的 Python 模板。单日期路径沿用上游策略：服务器端按日期汇总云量，最多评估前10个候选，同日空间拼接与云掩膜，按整个研究区计算有效覆盖率，达到阈值即可结束；不足时报告最佳候选与缺口，不宣称完整覆盖。季节/年度底图等任务可根据用途选用时间合成，不能把单日期观测与跨日期统计产品混淆。参考文档中的日期和 Sentinel-2 示例均是示例，不是全局默认。

研究区来自经核实的本地边界文件，行政区不能偷偷替换为市中心矩形。本地文件可以由流程自动准备，不要求用户事先提供：中国行政区任务读取 `china-admin-boundaries`，从保留原始坐标与来源的完整省、市、县数据中查询目标，按唯一名称或代码导出到会话目录；同名歧义先核对上下文，不能取第一条。技能附件通过 `gee_run_script` 读取 skill 工具返回的绝对目录，使用随应用打包的本地资源，不依赖运行时从 GitHub 下载。

`gee_run_script` 接收完整 Python 脚本字符串 `code`。执行前已按配置调用 `ee.Initialize(project=...)`；脚本可使用 `ee`、`output_dir`（`Path`）、`load_region`、`check_coverage`、`heartbeat`、`download_image` 和 `export_image_to_drive`。

```python
region = ee.Geometry.Rectangle([116.35, 39.90, 116.36, 39.91])
image = ee.Image("USGS/SRTMGL1_003").select("elevation")
path = download_image(image, "dem.tif", region=region, scale=30, crs="EPSG:4326")
print(path)
```

新增的流程帮助函数：

- `load_region(local_path)`：读取本地单要素 Polygon/MultiPolygon，统一到 EPSG:4326，返回 `ee.FeatureCollection`。沿用上游 GeoPandas 路径，检查 CRS、单要素、多边形类型及拓扑有效性，支持 GeoJSON/Shapefile/GeoPackage；缺依赖时明确提示，不跳过检查或自动安装。环境检查的 `region_ready` 单独说明边界读取依赖是否可用。
- `check_coverage(image, roi, band=None, scale=30, *, bands=None)`：返回 0–1 的浮点比例，与上游帮助函数一致；`bands` 可指定联合有效掩膜。分母来自完整研究区常量影像，避免仅覆盖一角的影像被误判为100%。默认30米检查是采样网格上的估计，不等于逐个10米输出像元验收。
- `with heartbeat("阶段名称"):`：记录阶段开始、耗时、结束或失败，并向界面反馈长时间计算仍在进行。它的输出会重置脚本的无输出计时；用户停止仍会中断本地执行。

`download_image` 使用 geedim 自动分块下载并拼成一个本地 GeoTIFF。Google 的 `getDownloadURL` 单请求上限为 32 MB、每个维度最多 10,000 像素；这是单块限制，不是整幅本地文件大小上限。代理不需要手写经纬度网格，也不应为绕过请求上限而降低分辨率。见 [Google 下载接口说明](https://developers.google.com/earth-engine/apidocs/ee-image-getdownloadurl)和 [geedim 下载 API](https://geedim.readthedocs.io/en/latest/reference/api.html)。

帮助函数接口为：

```python
download_image(image, filename, region=None, scale=None, crs=None,
               dimensions=None, *, dtype=None, max_requests=4,
               max_cpus=1, max_tile_size=16, reuse=True)
```

`dtype` 不填时沿用 geedim 识别的无损影像类型，例如 double 保留为 float64；显式填写才进行转换。下载不会自动强转 UInt16，不会擅自改变所给 `scale`。多分辨率波段导出到共同网格仍涉及重采样，应说明实际分辨率；合成图产生浮点数时，转整数也需要明确依据。

`max_tile_size` 按每片未压缩 MiB 计算，默认 16、允许大于 0 且不超过 32；`max_requests` 控制并发下载（1–16），`max_cpus` 控制并发解压（1–4）。大区域应使用 `region` 与 `scale`；兼容参数 `dimensions` 仍接受整数、`"宽x高"` 或 `[宽, 高]`，每维最多 10,000 像素。分块规模由实际输出网格、波段和 dtype 决定，不能只凭注释中的“UInt16”估算传输大小。

下载先写临时目录，所有块完成并通过输出元数据校验后才发布最终文件。任一块失败会清理未完成文件，记录失败原因；即使脚本捕获下载异常，工具也将结果标为 `incomplete`，不会因 Python 正常退出而宣称整次下载成功。多文件任务仍须核对预期数量、覆盖范围、波段和有效像元；已有局部成果不能代替完整研究区。下载阶段和百分比通过实时进度传递，记录在 `progress.jsonl`；进度完成也不能替代这些科学有效性检查。

同一会话中，下载器为本地序列化的影像表达式、研究区、分辨率、CRS、波段表达式及类型等生成精确请求标识。相同请求已有完整且未修改文件时直接返回原路径，不再次传输或复制；同时发起的相同请求会等待并共享该结果。新请求、文件丢失/改变和失败结果不会复用。显式 `reuse=False` 可重新取数。此保护是本地新增能力，不代表上游已有；不续传未完成瓦片，也不自动索引旧版本下载。

只有用户明确选择云端交付时，才使用 `export_image_to_drive(image, description, **kwargs)` 提交云端任务；本地下载失败不自动改走 Drive、STAC 或另一条大下载路线。先根据实际错误定位原因，修正有依据后再重试，避免重复任务。参数对应官方 `ee.batch.Export.image.toDrive`；任务 ID 会记录到结果和 `tasks.jsonl`。Drive 导出是异步批处理，详见 [Google 导出接口说明](https://developers.google.com/earth-engine/apidocs/export-image-todrive)。

- `READY` / `RUNNING`：已提交或处理中，不是已下载文件。
- `COMPLETED`：云端任务完成；结果在 Drive，不代表本地已有文件。当前没有自动 Drive 同步下载器。
- `FAILED` / `CANCELLED` / `UNKNOWN`：不得作为导出成功报告。
- 本地脚本超时或取消不会自动取消已提交的云端任务。先用 `gee_task_status` 查询保存的 `task_id`，避免重复提交；只有明确需要取消时才调用 `gee_cancel_task`，之后再查最终状态。

## 工具清单、输出与来源

| 工具 | 参数与作用 |
| --- | --- |
| `gee_check_environment` | 无参数；检查解释器、依赖、项目与已有认证是否可初始化。关闭 GEE 时也可检查。 |
| `gee_run_script` | `code`：Python 脚本；执行已授权的遥感分析与导出。 |
| `gee_task_status` | `task_id`：任务 ID 或 `projects/<project>/operations/<id>`；读取云任务真实状态。 |
| `gee_cancel_task` | `task_id`：发出取消请求，仍需查询确认最终状态。 |
| `browser_check_environment` | 无参数；只检查本地启动命令，不验证页面访问。关闭浏览器服务时也可检查。 |
| `browser_list_tools` | 无参数；启动/连接当前聊天的 MCP 服务，取得真实工具名和参数 schema。 |
| `browser_call_tool` | `tool_name`、`args` 对象；调用上一项实际返回的浏览器工具，不猜定位符和参数。 |
| `browser_close` | 无参数；关闭当前聊天的浏览器进程与会话。 |
| `web_read_page` | `url`，可选 `max_chars`；返回静态正文、链接和来源信息。 |
| `web_download_file` | `url`，可选 `filename`（仅文件名）；返回原始文件及来源记录。 |

执行工具需要服务启用及可信的当前聊天上下文；模型不能通过参数替换会话 ID 或输出根目录。环境检查例外，不需要已启用服务或当前会话输出目录。

输出根目录为 `<当前会话目录>/output/integrations/`，由系统从会话上下文计算：

```text
gee/run-*/             GEE 脚本、日志、任务记录和生成文件
browser/               当前聊天浏览器输出
downloads/download-*/  原始下载文件与 source.json
```

删除会话会永久移除整个会话目录，包括这里的影像、下载、脚本和日志，不进入废纸篓。删除前先取消并等待该会话的本地执行器退出；若未退出则保留文件并报告失败。已经另存到工程或其他目录的文件不会随会话删除，QGIS 中对应的图层也不会自动移除。

网页下载的 `source.json` 包含请求 URL、重定向后的 URL、获取时间、内容类型、实际字节数和 SHA-256；返回值还包含 `path`、`artifact_paths`、`manifest_path`。若下载文件恰好名为 `source.json`，来源记录改名为 `source-metadata.json`。每次下载新建目录，不覆盖已有文件。

GEE 保存实际脚本、有限长度的 stdout/stderr、任务记录、`progress.jsonl` 和 `artifact_paths`；脚本失败也保留诊断。结果的 `downloads` 逐项记录下载状态，成功项含实际路径、dtype、宽高、波段数和字节数，失败项保留错误码与原因。数据集、影像 ID、观测时间、ROI 来源、实际范围、分辨率和 CRS 应在脚本及最终说明中明确；这些下载记录仍不能替代任意 GEE 脚本的完整科学来源说明。临时签名下载 URL 不由帮助函数打印或保存。

浏览器返回页面内容、错误标志 `is_error` 和输出目录内发现的 `output_files`；列表可能截断，并不保证全部文件都是本次调用新生成的。浏览器下载没有自动附带网页下载工具那样的 SHA-256 来源清单，应保留页面 URL、操作信息和实际文件路径；可用公开直链时优先交给 `web_download_file`。传输错误或超时会关闭相应浏览器会话；下一次操作重新建立会话，可能需要重新登录。

## 下载回归验证（2026-10-06）

本机使用现有 Earth Engine 授权，验证了 Sentinel-2 影像 `20241224T030131_20241224T030910_T50RKV` 的矩形区域 `[114.2, 30.8, 114.45, 31.05]`。B4/B3/B2、10米、EPSG:32650、原生 UInt16，输出为 2458×2831 像素。下载器自动分为3块，未压缩数据41,751,588字节，最终GeoTIFF 40,475,898字节；本次从初始化到下载验收约24秒，所有块可读、所有波段像元有效，影像足迹包含完整测试区。这个小范围单景测试不代表全武汉或多时相合成也能在24秒完成。

另有无需 Google 账号的真实 geedim/rasterio 本地HTTP回归：9块 float64 影像逐像素与原始阵列一致。离线回归覆盖失败块、异常被脚本捕获、临时文件清理、超时后云任务ID保留、运行中日志与进度、取消及慢界面不会拖住执行。

## 开发者接入

配置路径为 `integrations` 整体对象，位于现有应用配置体系，建议通过界面保存。以下为默认值示意，不要求手工覆盖当前配置：

```yaml
integrations:
  gee:
    enabled: false
    python: python3
    project: ""
    proxy_url: ""
    timeout_seconds: 300
  browser:
    enabled: false
    command: npx
    args: ["-y", "@playwright/mcp@0.0.83"]
    browser: chrome
    headless: false
    timeout_seconds: 60
  web:
    enabled: false
    timeout_seconds: 60
    max_download_mb: 100
```

前端通过既有 `GET /api/config` 读取，并用 `PUT /api/config/batch` 写入 `{items:[{path:"integrations",value:完整对象}]}`，沿用统一控制面的校验、持久化和运行变更反馈，不直接写 YAML。超时配置为整数秒，统一配置允许 1–3600 秒；网页下载大小允许 1–1024 MB。

集成服务配置与智能体能力分配是两层设置。开启 GEE 不会让所有自定义智能体自动获得 GEE 工具；负责影像取数的运行时智能体还需在 `skill_packages` 中同时包含 `gee-remote-sensing` 工具包和 `gee-scripting` 工作流程；自动准备中国行政区边界还需 `china-admin-boundaries`。默认数据、空间分析和脚本专家均分配这三个包。现有方案通过智能体配置界面或对应 API 做增量修改，保留模型、提示词、PLAN 和其他能力包，不能用新版默认方案覆盖用户设置。

手动环境检查调用 `POST /api/integrations/check`，请求为 `{service:"gee"}` 或 `{service:"browser"}`。响应沿用控制契约与 API envelope，诊断对象位于 `data`，包含 `service`、`success`、`result` 和可选 `error`。HTTP 成功只表示检查请求已处理；还需看诊断状态、GEE 的 `environment.ready` 与 `environment.download_ready`，或浏览器的 `command_available`。GEE 下载诊断另提供 `geedim_version`（检测到兼容版本时）、`geedim_required_version` 及失败时的 `download_dependency_error`；不能把只可查询解释为可下载。

代码位置：

- `carto_agent/internal/config/integrations.go`：配置结构、默认值及校验。
- `carto_agent/internal/integrations/tools.go`：统一分发、会话输出目录、环境诊断。
- `carto_agent/internal/geeruntime/`：独立 Python 进程、GEE worker、下载和云任务状态。
- `carto_agent/internal/browserruntime/`：每会话 MCP 服务、生命周期、输出及错误转换。
- `carto_agent/internal/webdata/tools.go`：公开网页读取、限量下载、来源记录。
- `autocarto/tools/system/gee-remote-sensing/`、`autocarto/tools/system/browser-data-acquisition/`：工具契约。
- `autocarto/skills/system/gee-scripting/`：可读取的 GEE 工作流程、参考与模板；`gee-remote-sensing` 为工具分类包。浏览器能力见 `autocarto/skills/system/browser-data-acquisition/`。
- `autocarto-web/src/components/settings/IntegrationsConfigWorkspace.tsx`：设置卡片与显式环境检查。

本地检查与单元测试可覆盖配置、协议、超时和失败路径；真实登录、网络可达性、GEE 权限/配额、Drive 导出及网站交互仍须在用户自己的环境中联调。不要用模拟测试通过代替这些实际结果。

## GeoCode 流程对齐（2026-10-06）

对照 GeoCode v0.9.6 的运行代码及 gee-scripting 0.1.0 技能，补齐本地边界、覆盖检查、阶段心跳、影像处理参考和单日期候选模板。归因、版本及 MIT 许可保存在该技能的 `UPSTREAM.md` / `LICENSE` 中。

保留 AutoCarto 的独立 Python、已配置项目、QGIS 数据交接、无输出超时与用户取消，以及经验证的 geedim 2.0.0 分块下载。适配了上游模板将 `check_coverage` 浮点返回值误作字典的问题，并处理空集合与覆盖不足。对齐工作流程不代表所有自然语言请求都会作出相同模型决策，也不代表任意大区域下载都有固定耗时。


对齐后的真实验证：对武汉附近 `[114.2,30.8,114.45,31.05]` 测试区检索2024年12月（测试条件，非默认日期），返回4个候选日期，检查第1个即达标，选择2024-12-24两景同日拼接。30米覆盖检查为99.99%；下载10米RGB UInt16 GeoTIFF共38,679,781字节，从初始化至核验约40.15秒。逐块读取后，以投影后的完整测试ROI核对10米共同有效像元，比例99.9896%。栅格包围矩形中ROI外的无效像元不计入ROI覆盖分母。小范围测试不代表整个武汉的耗时。

本次在现有 GEE 独立虚拟环境补充 GeoPandas 1.1.1 及其依赖，EE 1.7.47、geedim 2.0.0 保持不变；环境检查分别报告查询、边界处理和下载就绪。未改变 QGIS 的 Python 环境或用户项目配置。


当时的异常路径真实验证：空候选与全掩膜候选均停止；仅半个测试区域有效的候选返回覆盖率50.11%、`meets_target=false`；筛选函数本身不下载或扩大条件。当前调用样例与上游对齐：有正有效覆盖的最佳候选可以下载，但明确说明缺口，并遵从用户另行指定的最低覆盖要求。离线检查包括27项Python测试（隔离GEE环境全部通过，包含真实GeoPandas几何校验和本地HTTP分块下载）、GEE运行时/集成及进度相关Go测试、29项设置与API前端测试。


2026-10-06 修复任务入口的技能加载错误：原 `gee-remote-sensing` 是 category 工具包，按现有架构不进入 skill 中间件的可读列表。将遥感正文和附件登记为独立的 `gee-scripting` workflow，并与原工具包同时分配给数据/空间分析/脚本专家，工具名称和执行权限不变；编排智能体负责路由，接收任务的专家读取流程。


入口回归使用真实默认配置、技能文件与 Eino 中间件，配合确定性测试模型跑通 `task → 专家 → skill → 返回`。三个执行专家均能读取正文、获得绝对目录并定位参考/模板，四个 GEE 工具保持授权；未分配工作流程的编排智能体仍被拒绝。测试不调用外部模型，也不重复下载影像。


2026-10-06 晚间完整复核：此前默认300秒总截止与 GeoCode 的无输出超时不同，导致整幅下载尚在传输就中断；已修正脚本的空闲计时与下载心跳。补全上游影像合成、分类、变化检测三份参考并记录固定提交与原文摘要；增加同会话精确请求复用及进程间协调、失败时完成文件清单恢复。删除会话在移除目录前等待本地执行器退出，并阻止迟到保存；外部工程数据不属于该删除范围。本轮以本地数据和隔离测试验证，不重复下载整市影像，不宣称任意模型规划问题已全部消除。

### 空工程边界准备复核（2026-10-06）

最新失败会话的工具记录为环境检查成功、工程和图层查询成功，随后请求边界并进入人工确认；没有执行 `gee_run_script`，也没有开始下载。之前遗漏了上游配套 `china-admin-boundaries`，仅要求可信本地边界，未提供空工程自动准备路径。现完整保留固定提交的34省、375市、2891县原始数据、技能原文和许可，增加按唯一身份选择并导出的本地适配；3300个原始几何逐个验证，选出的武汉多边形与上游逐值一致，投影面积约8588.17平方公里。

另一项独立问题是 `hitl_request` 的工具契约暴露空参数对象，适配层也未保留嵌套选项、计划和批量决策。现以已注册工具为前提使用原生结构化工具，并补全通用契约和参数解码；通过实际 registry→engine→ADK 中断/恢复验证三类请求的完整字段。不增加未分配的人工确认工具。

真实模型回归还发现主智能体此前直接拿到完整执行工具集，尽管没有分配 GEE 包，仍可绕过专业委派和工作流程。现在主智能体与专家使用相同的配置工具过滤；专家构造和能力说明仍用完整工具集，用户明确分配给主智能体的能力仍保留。未配置运行时的旧显式工具调用保持兼容，不针对城市或 GEE 工具名称硬编码。低时限并行回归发现空闲计时此前包含日志准备和 OS 启动，现从子进程成功启动后开始计时；父取消始终有效，用户时限没有放宽。

| 验证环节 | 实际结果与边界 |
| --- | --- |
| 边界包 | 10项离线测试通过；全部3300原始要素有效；重复名称不猜测，源字节/字段/CRS错误停止。武汉导出能由实际 GEE 区域读取器读取。 |
| 当前模型的空工程路由 | 使用当前 live default 配置与模型，输入原句“使用gee 下载武汉市遥感数据”，不补写卫星、日期或代码。实际走 `get_project_info→task(data_engineer)→skill(gee-scripting)→skill(china-admin-boundaries)→环境检查→读参考→查询并导出武汉边界`，没有人工确认请求。模型请求真实执行；QGIS/EE 工具拦截模拟，只将模型选出的身份交给可信模板实际导出。因此它验证路由与边界阶段，不等同于模型自主完成整市下载。 |
| 真实 GEE 查询和下载 | 由 Go `Invoke` 启动真实 worker；从原始武汉多边形内部选取1平方公里验收样区。2024年12月共6候选日期，第1个2024-12-24覆盖100%，2景同日拼接；10米RGB、EPSG:32650、101×101、UInt16、52,791字节，初始化到核验约15.17秒。日期和样区仅用于测试，不是产品默认条件。 |
| 跨进程重复请求 | 第二次启动 worker，反序列化同一影像和区域表达式，返回第一份成果路径，`reused=true`，未创建第二张栅格；约5.65秒含 EE 初始化。 |
| QGIS 真实工具加载 | 本机 QGIS 3.44.10 的独立无界面进程读取当前运行时 manifest，经实际 `QGISToolEngine→data_load_raster` 加载成功；三个波段全部像元读取有效，分辨率、CRS和数值检查通过，测试层随后移除。没有打开或修改用户工程。 |
| 验证限制 | 小样本不代表整武汉下载耗时，独立 QGIS 进程不覆盖可视界面渲染和 WebSocket 传输；没有为验收额外运行整市下载、Drive 导出或全部分类/变化检测算法。 |

正式当前模型路由回归最终通过，耗时124.62秒（包含多轮模型请求；不是影像下载耗时）；配置、模型凭据和项目文件核对未被测试修改。主工具过滤、显式分配与 `agent_creator` 自定义主名兼容回归，以及 GEE 超时/取消、会话删除、中断恢复相关检查通过。21:10更新后运行时 BUILD/PLAN 均就绪、QGIS 保持连接，打包的边界数据/模板/工具契约与源码一致；通过正在运行的服务再查 GEE，查询、边界、下载均就绪。本轮52,791字节测试栅格已删除，保留报告，不额外留存测试影像。
