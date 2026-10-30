# 04 geo 运行时规范

geo 运行时承接 204 项工具：QGIS 版里所有“文件进、文件出”的计算，原来由 QGIS Processing 或 Python handler 执行。它与宿主无关，可在 Linux CI 测试，以后也可以被 QGIS 版复用。

## 1. 为什么单独一个运行时

- MapGIS 10 .NET 的空间分析接口没有公开文档，且是几何级接口（`SpaAnalysis.buffer`、`intersection`、`clip` 等，见 `evidence/mapgis-api-参考.md` §4）。逐个封装成图层级工具，工作量与原生崩溃风险都很高。
- QGIS Processing 的 `native:*`、`gdal:*` 算法建立在 GEOS、GDAL、PROJ 之上；geo 运行时用同一套库（shapely 2.1、GDAL、pyproj），能逐项与 QGIS 参照输出对照（§11、V71）。
- 现有纯 Python 逻辑可以直接复用，不必重写：路线知识 `autocarto/knowledge/`、统计准备 `_shared/statistical_data_impl.py`。已实测这些模块在没有 QGIS 的 Python 中能导入（`evidence/00-对比清单.md` §5）。
- 取数阶段的规范化和精裁不再依赖宿主在线。

## 2. 环境

用 Miniforge 和 conda-forge 建独立环境 `autocarto-geo`。conda-forge 的 GDAL、PROJ、GEOS、PDAL 出自同一构建体系，避免 pip 轮子各自带一份 GDAL 时的冲突。

`autocarto_mapgis/geo/environment.yml`：

```yaml
name: autocarto-geo
channels: [conda-forge]
dependencies:
  - python=3.11
  - gdal>=3.9          # 含 osgeo.gdal / ogr / osr，SQLite 方言需 SpatiaLite（conda-forge 构建已含）
  - pyogrio>=0.10
  - rasterio>=1.4
  - geopandas>=1.0
  - shapely>=2.1       # coverage_* / orient_polygons / make_valid(method=structure) 需要 GEOS ≥ 3.12
  - pyproj>=3.6
  - numpy>=1.26
  - scipy>=1.13
  - scikit-learn>=1.5
  - networkx>=3.3
  - pysheds>=0.4
  - pdal>=2.7
  - python-pdal>=3.4
  - matplotlib>=3.9
  - pillow>=10
  - pymupdf>=1.24      # 版面 PDF → SVG
  - openpyxl>=3.1
  - ezdxf>=1.3
  - xarray>=2024.6
  - netcdf4
  - py7zr
  - pyyaml
  - pytest
```

- **锁定版本**：M03 用 `conda-lock` 生成 `environment.lock.yml`（win-64、linux-64 两个平台），安装脚本只按锁文件安装。
- **Windows 安装**：`scripts/geo_env.ps1 install|verify`，装到 `%LOCALAPPDATA%\AutoCarto\conda\envs\autocarto-geo`，不改系统 PATH。
- **Linux CI**：`scripts/geo_env.sh` 安装同一锁文件。
- **自检**：`python -m autocarto_geo.doctor` 输出 JSON，含各依赖版本、GEOS 版本、GDAL 驱动列表（GPKG、GeoJSON、ESRI Shapefile、GTiff、DXF、GPX、SQLite）、中文字体可用性、写临时文件是否成功。Go 启动时调用一次，结果决定目录可用性（`01` §3）。
- **配置**：`config.yaml` 新增：

  ```yaml
  geo:
    enabled: true
    python: ""            # 为空时按 %LOCALAPPDATA%\AutoCarto\conda\envs\autocarto-geo\python.exe → 配置的 conda 环境名 依次查找
    workers: 2
    timeout_seconds: 600  # 单次调用缺省；契约 execution.timeout 可覆盖
    recycle_after_calls: 50
  ```

- **隔离**：进程环境去掉 QGIS 或 MapGIS 带来的 `PYTHONHOME`、`PYTHONPATH`，做法同 `geeruntime/runtime.go:350` 的处理。

## 3. 进程模型与协议

Go 侧位于 `carto_agent/internal/georuntime/`，由 H16 实现：

- **进程池**：维持 `workers` 个常驻 worker，按请求分配。每个 worker 处理 `recycle_after_calls` 次后重启；常驻内存超过 2 GB 时，处理完当前请求就重启。
- **启动命令**：`<python> -I -m autocarto_geo.worker --root <资产根>`，工作目录为会话目录。`-I` 让解释器不从当前目录加载模块。
- **通信**：
  - 标准输入输出逐行 JSON；标准错误写 worker 日志 `<home>/data/logs/geo-worker-<pid>.log`。
  - 请求：

    ```json
    {"id": "req-…", "tool": "buffer", "handler": "autocarto_geo.vector.geometry:buffer",
     "params": {"INPUT": "…/materialized/a1b2.gpkg", "DISTANCE": 500, "OUTPUT": "…/output/work/buffer-3.gpkg"},
     "context": {"session_dir": "…", "output_dir": "…", "temp_dir": "…", "project_root": "…",
                 "layers": {"INPUT": {"layer_id": "ml_…", "crs": "EPSG:4547", "name": "学校"}},
                 "timeout_seconds": 600, "network": "deny"}}
    ```

  - 响应：

    ```json
    {"id": "req-…", "success": true,
     "output": {"success": true, "output": "缓冲完成：128 个要素", "info": {}, "OUTPUT": "…/buffer-3.gpkg"},
     "output_layers": [{"path": "…/buffer-3.gpkg", "layer_name": "学校_缓冲500米", "kind": "vector", "auto_load": true}],
     "error": "", "error_code": "", "warnings": []}
    ```

- **进度**：长任务可以输出 `{"id": …, "progress": 0.4, "message": "…"}` 行，Go 转为运行事件。
- **取消与超时**：取消请求直接结束该 worker 的进程组（Windows 用 Job Object，同 `geeruntime/process_windows.go`），然后补起新 worker。半成品文件留在 `temp_dir`，不登记为成果。
- **脚本工具**：G57 的 `geo_*` 每次单独起一个进程，不进常驻池，按 `01` §10 的限制运行。

## 4. 包结构

```text
autocarto_mapgis/geo/
├── environment.yml / environment.lock.yml
├── autocarto_geo/
│   ├── worker.py              协议循环、处理器分发、异常转回执
│   ├── doctor.py              自检
│   ├── params.py              按契约参数类型转换（§5）
│   ├── io.py                  矢量/栅格读写（pyogrio、rasterio），编码、坐标系、字段类型映射
│   ├── publish.py             输出描述构造（§6）
│   ├── crs.py                 坐标系工具与 crs-projection 处理器
│   ├── expr/                  表达式方言（§7）：lexer.py、parser.py、evaluator.py、functions.py
│   ├── vector/                geometry.py、creation.py、fields.py、selection.py
│   ├── raster/                analysis.py、visibility.py、profile.py
│   ├── analysis/              spatial.py、network.py
│   ├── gdal_tools.py          gdal-processing 处理器
│   ├── grass_compat.py        grass-analysis 等价实现
│   ├── pointcloud.py          PDAL
│   ├── charts.py              图表
│   ├── mesh.py                2DM / UGRID 网格
│   ├── convert.py             data-conversion
│   ├── files.py               打包、分割、GPX 等
│   ├── io_tools.py            geodata_inspect / thumbnail / layer_inspect / data_export_*
│   ├── acquire/               archive.py、derive.py、normalize.py、admin_boundary.py
│   ├── stats.py               包装 autocarto.tools.system._shared.statistical_data_impl
│   ├── knowledge.py           包装 autocarto.knowledge.handler
│   ├── style_support.py       热力栅格、点位移副本等样式辅助
│   └── scripting/             geo 脚本工具（G57）
└── tests/                     pytest；fixtures/ 与 reference/（QGIS 参照输出）
```

- **共享模块**：worker 启动时把项目根放到 `sys.path` 最前，然后只读导入：
  - `autocarto.knowledge`；
  - `autocarto.core.paths`；
  - `autocarto.tools.system._shared` 下的 `resource_handle`、`statistical_data_impl`、`table_io`、`tool_assets`、`tool_presets`。
- **发行包**：只带上述模块的源码（W22），不带 QGIS 插件的其他部分。
- **守卫测试**：在没有 QGIS 的环境中导入上述模块，要求成功，且 `sys.modules` 中没有 `qgis`。

## 5. 契约写法与参数类型

MapGIS 包中 geo 工具契约的写法：

```yaml
metadata:
  name: buffer
  runtime: geo
  requires: []                 # 例如 [pdal]、[pysheds]
spec:
  parameters: …                # 与 QGIS 版逐项相同
execution:
  handler: autocarto_geo.vector.geometry:buffer
  timeout: 600s
  output:
    auto_load: true            # 输出由系统发布（01 §6.3）
  param_mapping: …             # 与 QGIS 版相同
```

参数类型按 QGIS 执行器的规则转换（`autocarto/core/tool_engine/executor.py:643-830`），由 `params.py` 统一处理：

| 类型 | 转换 |
|---|---|
| `layer`、`vector_layer`、`raster_layer`、`multi_layer` | Go 已物化为文件路径（`01` §6.2）；处理器收到的是路径，原图层信息在 `context.layers` |
| `output_layer`、`output_raster` | `memory:` 或空值：Go 已换成会话工作路径；扩展名决定格式（.gpkg、.geojson、.shp、.tif） |
| `field`、`fields` | 字段名或字段名数组，大小写不敏感匹配；匹配不到时报 `invalid_params` 并列出现有字段 |
| `enum` | 整数序号，含义与 QGIS 算法完全相同（每个处理器在 docstring 中列出序号表）；字符串形式按选项名接受 |
| `matrix` | 一维数组，按 QGIS 规则分组（例如重分类表每 3 个值为一组：最小、最大、新值） |
| `crs` | `EPSG:xxxx`、WKT 或 `"layer"` |
| `expression` | 字符串，按 §7 的方言 |
| `number`、`integer`、`boolean`、`string` | 同 QGIS 执行器的 `_coerce_param_value` |

## 6. 与 QGIS 执行器对齐的公共行为

| 行为 | QGIS 版位置 | geo 运行时 |
|---|---|---|
| 几何无效时自动修复并重试一次 | `executor.py:313` `_retry_with_fixed_geometries_once` | `vector/geometry.py` 的公共装饰器：捕获 GEOS 拓扑错误后，对输入做 `make_valid` 再执行一次，回执带 `retry_diagnostic` |
| 重投影请求与结果校验 | `executor.py:191,235` | `crs.py`：目标坐标系无效时拒绝；结果坐标系与请求不符时报错 |
| 输出发布、返回图层 ID | `executor.py` `_publish_algorithm_outputs` | 由 Go 发布（`01` §6.3），处理器只给出描述 |
| 默认输出名 | `executor.py:967` `_generate_default_output_name` | Go 生成路径；`layer_name` 用“输入名_操作”的格式，与 QGIS 版一致 |
| 空结果 | 成功并说明 0 个要素 | 同；输出文件仍写出（带字段结构），便于后续步骤 |
| 面积与长度 | 投影坐标系为平面量；地理坐标系为椭球量 | 同：投影坐标系用 shapely，地理坐标系用 `pyproj.Geod(ellps="WGS84")` |
| 字段类型 | QGIS 字段类型 | pyogrio：整数 int64、浮点 float64、文本 str、日期 datetime；Shapefile 字段名超过 10 个字符时报警告 |
| 属性编码 | OGR 按 `.cpg` 读取 | 同；没有 `.cpg` 的 Shapefile 先按 UTF-8 尝试，失败再按 GBK，并在回执中写明实际编码 |

## 7. 表达式方言

QGIS 表达式是 QGIS 专有语言。MapGIS 版使用它的常用子集，C# 实现（P34，宿主选择与规则样式）与 Python 实现（本节）共享一套用例 `autocarto_mapgis/spec/expressions/*.json`，两边测试都必须全部通过。

- **字面量**：数字、`'字符串'`、`TRUE`、`FALSE`、`NULL`。
- **字段引用**：`"字段名"`，或与字段同名的裸标识符。
- **运算符**：
  - 算术与连接：`+ - * / % ^`、`||`；
  - 比较：`= <> != < <= > >=`；
  - 逻辑：`AND OR NOT`；
  - 集合与匹配：`IN (…)`、`NOT IN (…)`、`LIKE`、`ILIKE`、`IS NULL`、`IS NOT NULL`、`BETWEEN … AND …`；
  - 条件：`CASE WHEN … THEN … [ELSE …] END`。
- **函数**（参数可以按位置，也可以用 `名称:=值`）：
  - 数学：`abs`、`round(x[,n])`、`floor`、`ceil`、`sqrt`、`pow`、`exp`、`ln`、`log10`、`min`、`max`、`clamp`、`pi`；
  - 字符串：`length`、`upper`、`lower`、`trim`、`title`、`substr(s,start[,len])`、`left`、`right`、`replace`、`regexp_replace`、`regexp_match`、`strpos`、`lpad`、`rpad`、`concat`、`format_number`；
  - 转换与条件：`to_int`、`to_real`、`to_string`、`coalesce`、`if(cond,a,b)`、`nullif`；
  - 几何：`$area`、`$length`、`$perimeter`、`$x`、`$y`、`$geometry`、`area(g)`、`length(g)`、`perimeter(g)`、`x(g)`、`y(g)`、`centroid(g)`、`buffer(g,d)`、`num_points(g)`；
  - 要素：`$id`、`@row_number`；
  - 聚合：`sum`、`mean`、`minimum`、`maximum`、`count`，支持 `group_by:=`、`filter:=`；
  - 日期：`now()`、`year()`、`month()`、`day()`。
- **不支持的函数与语法**：报 `unsupported_parameter`，错误信息列出可用函数。不静默返回 NULL。

工具契约说明里的“QGIS 表达式”，在 MapGIS 包中统一改为“表达式（QGIS 常用子集，见工具包说明）”。`vector-fields`、`vector-selection` 工具包附一页方言说明（R65）。

## 8. 算法对照要点

逐工具的去向见 `02`。这里列各族的实现库和需要特别注意的参数。参数的序号与语义一律以 QGIS 算法帮助为准：在装有 QGIS 的机器上用 `processing.algorithmHelp('<算法 ID>')` 查看，G50 把输出存到 `geo/tests/reference/algorithm-help/`。

| 族 | 实现 | 需要注意 |
|---|---|---|
| 缓冲与叠加（`buffer`、`clip`、`intersection`、`difference`、`union`、`symmetric_difference`、`dissolve`） | shapely、geopandas.overlay | 端点样式 0 圆、1 平、2 方，连接样式 0 圆、1 斜接、2 斜角，与 QGIS 相同；`intersection` 的字段保留与 `OVERLAY_FIELDS_PREFIX` 前缀规则同 QGIS；`dissolve` 的 `SEPARATE_DISJOINT` 用 explode 实现 |
| 构造与提取（质心、地表点、最大内切圆中心、外包、凸包、凹包、Delaunay、Voronoi） | shapely 2.1：`centroid`、`point_on_surface`、`maximum_inscribed_circle`、`envelope`、`oriented_envelope`、`minimum_bounding_circle`、`concave_hull`、`delaunay_triangles`、`voronoi_polygons` | Voronoi 的 `BUFFER` 按包络百分比外扩；`minimum_bounding_geometry` 按 `FIELD` 分组、`TYPE` 0–3 |
| 线面编辑（加密、简化、平滑、捕捉、偏移、单侧缓冲、渐变或楔形缓冲、延长、合并、分割、子线段、沿线取点、拆分） | shapely `segmentize`、`simplify`、`snap`、`offset_curve`、`buffer(single_sided)`、`line_merge`、`ops.split`、`ops.substring`、`line_interpolate_point`；平滑用 Chaikin，参数 `ITERATIONS`、`OFFSET`、`MAX_ANGLE`；渐变、楔形缓冲按 QGIS 算法说明实现 | `simplify` 的 `METHOD`：0 为 Douglas–Peucker，1 为吸附到格网，2 为 Visvalingam，需自行实现；`subdivide` 用四叉分割，`MAX_NODES` 同 QGIS |
| 有效性 | `is_valid_reason`、`make_valid(method="linework"/"structure")`、`orient_polygons`（右手法则） | `check_validity` 输出合法、非法、错误点三个图层，字段同 QGIS |
| 覆盖类 | `coverage_simplify`、`coverage_union`、`coverage_invalid_edges` | 需要 GEOS ≥ 3.12；doctor 检查 |
| 几何属性 | 面积、周长、长度、坐标；`CALC_METHOD` 0 图层坐标系、1 工程坐标系、2 椭球 | “工程坐标系”取宿主当前地图的坐标系，由 Go 放进 `context.project_crs` |
| 字段 | geopandas | 自增字段的分组与排序表达式；按位置连接的谓词序号 0 相交、1 包含、2 相等、3 接触、4 重叠、5 在内、6 穿越，与 QGIS 相同；最近连接用 `sjoin_nearest`，参数 `NEIGHBORS`、`MAX_DISTANCE`；`text_to_float`、`refactor_fields` 的字段映射结构同 QGIS 版处理器（`_shared/vector_attributes_impl.py`） |
| 按属性提取 | geopandas | 运算符序号：0 =、1 ≠、2 >、3 ≥、4 <、5 ≤、6 开头为、7 包含、8 为空、9 非空、10 不包含 |
| 创建 | 格网 `TYPE` 0 点、1 线、2 矩形、3 菱形、4 六边形；随机点 `MIN_DISTANCE`、`SEED`；规则点 | 随机结果给定种子时可复现，但不要求与 QGIS 逐点相同（保真度为“等价”时只比较数量与分布约束） |
| 转换 | `rasterio.features.shapes`（四连通，同 `gdal:polygonize` 默认）、`gdal.Rasterize`、ezdxf 或 GDAL DXF 驱动 | 栅格转矢量的字段名 `DN` |
| 坐标系 | `GeoDataFrame.to_crs`；写 `.prj`；候选坐标系按目标范围打分 | 重投影后由宿主设置图层参照系（`CrsMapper`） |
| GDAL 处理 | osgeo.gdal：`Warp`、`Translate`、`DEMProcessing`（slope、aspect、hillshade、TRI、TPI）、`ContourGenerate`、`BuildVRT`、`VectorTranslate`（按范围或多边形裁剪）、SQLite 方言（缓冲、融合、偏移曲线） | 与 QGIS gdal provider 的参数名、缺省值一致；SQLite 方言需要 SpatiaLite，不可用时改用 shapely 并标“近似” |
| 栅格分析 | numpy、rasterio | 重分类 `RANGE_BOUNDARIES`：0 为 min<v≤max，1 为 min≤v<max，2 为 min≤v≤max，3 为 min<v<max；`NODATA_FOR_MISSING`；统计输出 MIN、MAX、RANGE、SUM、MEAN、STD_DEV、SUM_OF_SQUARES（排除无数据值）；热力为四次核，参数半径、像元、权重字段、输出比例 |
| 插值 | IDW 用距离系数 P；TIN 用 `scipy.interpolate.LinearNDInterpolator` 或 `CloughTocher2DInterpolator` | 近似：输出与 QGIS 的差异统计写入 V71 |
| 空间统计 | scikit-learn 的 DBSCAN、KMeans（k-means++，`n_init` 固定，随机种子固定）；scipy `cdist`、`cKDTree` | 聚类编号可能与 QGIS 不同；对照时比较分组是否一致，不比较编号 |
| 地形 | GDAL TRI（Riley）、TPI | — |
| 网络 | networkx：按 `TOLERANCE` 节点化并建有向图；`STRATEGY` 0 最短、1 最快；`DIRECTION_FIELD`、`VALUE_FORWARD`/`BACKWARD`/`BOTH`、`DEFAULT_DIRECTION`、`SPEED_FIELD`、`DEFAULT_SPEED`（km/h） | 起止点吸附到最近边上的点，不只吸附到节点；服务区输出可达边，含部分边，与 QGIS 一致；`INCLUDE_BOUNDS` 输出边界点 |
| 可视域 | `gdal.ViewshedGenerate`（观测高、目标高、最大距离）；视线沿线采样 | 移植 `_shared/visibility_impl.py` 的逻辑，把 QGIS 栅格读取换成 rasterio |
| GRASS 等价 | `r.contour` 用 GDAL 等值线；`r.slope.aspect` 用 DEMProcessing；`r.watershed` 用 pysheds（填洼、流向、汇流累积、流域）；`v.buffer` 用 shapely；`v.clean` 用 `make_valid` 加节点吸附（降级）；`v.generalize` 用 Douglas–Peucker 或 Chaikin | 契约说明中注明“GDAL/pysheds 等价实现，非 GRASS” |
| 点云 | PDAL 管线：`readers.las`、`filters.crop`、`filters.merge`、`writers.las`、`writers.gdal` | 未安装 PDAL 时，目录标 `geo_dependency_missing:pdal` |
| 图表 | matplotlib：同时输出 HTML（兼容 QGIS 版 `OUTPUT`）和 `svg_path`/`picture_path`，供 `layout_add_picture` 直接使用 | 中文字体按 `host.yaml` 的候选查找 |
| 网格 | 自写 2DM 读写；UGRID NetCDF 用 xarray | 只支持这两类网格文件，其他 MDAL 格式返回 `unsupported_parameter` |
| 取数处理 | 移植 `data-acquisition/handlers/{normalize,derive,archive}.py` 与 `_shared/admin_boundary_impl.py`：把 `QgsVectorLayer`、`QgsGeometry`、`QgsCoordinateTransform` 换成 geopandas、shapely、pyproj | 输出字段、标准字段、简化容差（米：先投影到局部坐标系）、`merge_by`、`min_length_m`、`min_area_m2`、`merge_distance_m` 的语义与 QGIS 版逐项一致；用 QGIS 版的测试用例（`autocarto/tests/test_data_normalize_clip_qgis.py` 等）做对照 |
| 检查与缩略图 | pyogrio `read_info`、`list_layers`；rasterio；matplotlib | `geodata_thumbnail` 是示意渲染，不是宿主渲染 |
| 统计与路线知识 | 直接调用共享模块 | 回执结构不变 |

## 9. 输出的写法

- 矢量缺省写 GeoPackage：图层名与文件名相同，坐标系写入文件。
- 用户指定 `.shp` 时同时写 `.cpg`（UTF-8）。
- 栅格缺省写 GeoTIFF：DEFLATE 压缩，按 512 像素分块；有无数据值时写入。
- 输出文件已存在时先写临时文件，成功后原子替换（与 `data_export_vector` 的 `replace_source` 规则一致）。

## 10. 安全与网络

- 常驻 worker 默认禁止网络：不设代理，并拦截 `socket.connect`。需要联网的取数由 Go 内建工具完成，geo 只处理已下载的文件。
- 文件写入只允许会话目录和参数中显式给出的输出路径。参数里的路径必须在项目根、会话目录或用户提供的目录之下，这一检查由 Go 执行（沿用现有输入路径规则）。

## 11. 参照输出与测试

- **参照生成**：`scripts/make_geo_reference.py` 在装有 QGIS 的机器上运行（Linux、macOS、Windows 均可）。它对 `geo/tests/fixtures/` 中的每组输入，按契约参数调用 QGIS Processing 或 QGIS 版处理器，把输出与回执写到 `geo/tests/reference/<工具>/<用例>/`。参照随仓库提交，geo 测试本身不需要 QGIS。
- **对照规则**：
  - 等价：要素数相同；属性逐项相同（浮点相对误差不超过 1e-9，或按字段精度）；几何对称差面积不超过 0.1%，长度差不超过 0.1%；栅格逐像元差不超过 1e-6，或按数据类型精度。
  - 近似：记录差异统计，要求差异落在契约说明写明的范围内。
- **用例数量**：每个工具至少一组正常用例和一组边界用例（空输入、无效几何、地理与投影两种坐标系、含中文字段）。
- **表达式**：共享用例覆盖 §7 的全部语法与函数，Python 与 C# 两边都跑。
- **运行**：`conda run -n autocarto-geo pytest autocarto_mapgis/geo/tests -q`；Linux CI 与 Windows 都跑。

## 12. 性能目标

| 操作 | 规模 | 目标 |
|---|---|---|
| worker 冷启动（导入 geopandas、rasterio） | — | ≤ 3 秒，常驻后不再计入 |
| 缓冲加融合 | 10 万个面 | ≤ 30 秒 |
| 栅格转矢量加融合 | 300 万像元、5 类 | ≤ 60 秒 |
| 物化导出（宿主 → GeoPackage） | 10 万个面 | ≤ 20 秒（由 T41 保证） |

超出目标的情况写入 V71 记录，不阻塞验收，但要给出原因。
