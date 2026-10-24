# T53 A1/A2：真实流程控制器与 QGIS 成果验收

本记录只覆盖 A1/A2。A3 界面与 A4/A5 最终门禁由主代理汇总到最终 T53 证据，不能把本文件的通过解释为全部重构验收完成。

## 验收方式及边界

新增通用离线验收通道：`carto_agent/integration/acceptance/main.go`、`autocarto/tests/integration/acceptance/run.py` 与 `qgis_worker.py`。真实读取当前默认 v2 方案、正式 category、authority manifest、FLOW.md 和资源，经过真正的 `flow.Compiler`、`flowrun.Controller`、`FileRunStore`、条件判断、产出校验、事件和落盘。每步确定性角色适配器按动作资源调用真实 QGIS handler，调用时检查负责角色的实际工具权限；控制器通过真实 `get_layers_info`、`layout_info` 通道核验图层/版面，并实际检查文件和审查结构。

**没有调用真实 LLM。** 编译模型与角色动作采用固定夹具，以便离线重现；不检验模型理解自然语言、规划、视觉判断或工具选择的可靠性。质量分支真实完成 PNG 导出、布局快照、原图保护、终稿预留、140 DPI 同尺寸连续导出及双图加载，但八维和最终结论是明确标注的确定性结构夹具；`final_passed: true` 只用于验证结构和流程推进，不能当成真实多模态审图通过。

没有调用 `run_legacy_route`、`run_legacy_statistical` 或旧 golden driver 代替流程执行。只复用 T01 的快照/预览导出与比较函数；全部制作步骤由控制器逐步触发。A1 使用原场景的显式视觉参数夹具（旧图层/图例名称、原综述、关闭旧场景没有的网格），与 T43/T44 黄金适配一致；不声称固定适配器证明了 FLOW 正文每一条自然语言策略都能被模型自主执行。

成果持久保存在项目内忽略目录：

- [完整五场景结果](../../../autocarto/tests/golden/.runs/T53/attempt-03/results.json)
- [验收脚手架](../../../autocarto/tests/integration/acceptance/run.py)
- [真实 Controller 装配](../../../carto_agent/integration/acceptance/main.go)
- [真实 QGIS 角色适配器](../../../autocarto/tests/integration/acceptance/qgis_worker.py)

每个场景包含 `plan.json`、真正的 `flows/<flow_run_id>/run.json`、`events.jsonl`、`transcript.jsonl`、`tool-calls.json`、源数据前后哈希、实际 PNG/PDF、预览与版面快照。`scenario.json` 和 `controller.log` 记录实际输入和执行。保留首次失败及修复重跑的 attempt-01、attempt-02，没有覆盖失败证据。

## A1：内置流程的旧场景不退化

| 场景 | 控制器结果与分支 | 原黄金完整比较 | 持久成果 |
| --- | --- | --- | --- |
| 路线，关闭质量审查 | completed；s1 因现成数据集跳过，s2–6、s11 完成，s7–10 跳过；16 路线、81 点、2 图文注记 | `differences=[]`；dHash=1，仅诊断 | [run.json](../../../autocarto/tests/golden/.runs/T53/attempt-03/route-no-review/flows/fr-20261021-111943-28674693a182/run.json)、[比较报告](../../../autocarto/tests/golden/.runs/T53/attempt-03/route-no-review/comparison.json)、[PDF](../../../autocarto/tests/golden/.runs/T53/attempt-03/route-no-review/exports/固定路线数据回归样例.pdf)、[预览](../../../autocarto/tests/golden/.runs/T53/attempt-03/route-no-review/preview.png) |
| 路线，开启质量审查 | completed；s1 跳过，s2–10 完成，s11 跳过；140 DPI 初版与终稿；原图哈希保护及预留终稿验证通过 | `differences=[]`；dHash=1，仅诊断 | [run.json](../../../autocarto/tests/golden/.runs/T53/attempt-03/route-with-review/flows/fr-20261021-112000-6780af67f18a/run.json)、[比较报告](../../../autocarto/tests/golden/.runs/T53/attempt-03/route-with-review/comparison.json)、[PDF](../../../autocarto/tests/golden/.runs/T53/attempt-03/route-with-review/exports/固定路线数据回归样例.pdf)、[预览](../../../autocarto/tests/golden/.runs/T53/attempt-03/route-with-review/preview.png) |
| 统计专题图 | completed；4 步全部通过；17 个面，16 个有效值、1 缺测、1 明确报告的未匹配表地区；零值保留，五级分位数及真实图例 | `differences=[]`；dHash=0，仅诊断 | [run.json](../../../autocarto/tests/golden/.runs/T53/attempt-03/statistical/flows/fr-20261021-112022-4c5f51e07096/run.json)、[比较报告](../../../autocarto/tests/golden/.runs/T53/attempt-03/statistical/comparison.json)、[PNG](../../../autocarto/tests/golden/.runs/T53/attempt-03/statistical/exports/统计成果.png)、[预览](../../../autocarto/tests/golden/.runs/T53/attempt-03/statistical/preview.png) |

三个真实计划 `diagnostics=[]`。所有执行尝试的控制器 `Verification.OK=true`；文件、图层和版面并非仅由适配器声称存在。版面工具返回 warnings 均为空。没有修改固定输入、原黄金基线、字号容差、1 mm 几何阈值或比较忽略项；新标题加粗、锁字号及中心锚点另有实际断言。

## A2：新场景只增加 FLOW.md 和资源

通用验收器不按这两个流程编号/名称添加业务分支。两包只增加 `FLOW.md` 和资源，包括离线角色动作 JSON；没有修改任何生产工具、运行时或默认方案。固定角色动作是模型行为的离线替身，因而此验收证明文档/资源结构及现有工具足以完成新场景，不能外推为任意模型都能自主完成。

### 省级人口密度分级设色图

- 包：[FLOW.md](../acceptance-flows/province-density/FLOW.md)。完整 34 省原始行政区面，固定来源和许可；人口、面积均明确标注为合成验收数据，图标题和说明也明确标注，不能用作真实人口分析。
- 真实六步：清单加载 → 按行政代码精确关联 CSV → 独立 Double 字段计算 `population/area_km2` → 读取 34 个实际字段逐一核对公式 → Jenks 自然断点五级 → 统计专题版式 → PNG/PDF。加载、连接、字段计算由现有数据/属性工具完成。
- 真实计划无诊断，六步 completed、全部产出校验通过；五种不同颜色、34 个有效值、零空值；布局 warnings 为空。
- [run.json](../../../autocarto/tests/golden/.runs/T53/attempt-03/province-density/flows/fr-20261021-112023-77c459006c63/run.json)、[PNG](../../../autocarto/tests/golden/.runs/T53/attempt-03/province-density/exports/人口密度.png)、[PDF](../../../autocarto/tests/golden/.runs/T53/attempt-03/province-density/exports/人口密度.pdf)、[实际字段与工具回执](../../../autocarto/tests/golden/.runs/T53/attempt-03/province-density/adapter-variables.json)、[版面预览](../../../autocarto/tests/golden/.runs/T53/attempt-03/province-density/preview.png)。已目视检查预览，标题、实际五级图例和合成说明完整。

### 考察路线图

- 包：[FLOW.md](../acceptance-flows/survey-route/FLOW.md)。独立自备六表：南京—宣城—杭州 3 站点、1 条有向考察路线、普通站点圆形 SVG、空注记表；完整区域面按代码筛选江苏/浙江/安徽，没有坐标转换或修复。合成安排说明明确保留。
- 使用本包单色路线 CSV、独立路线/站点工具和四角对齐版式；真实创建 3 站点、1 路线与名称标注。四步 completed、计划无诊断、产出核验通过、布局 warnings 为空，输出 PNG/PDF。
- [run.json](../../../autocarto/tests/golden/.runs/T53/attempt-03/survey-route/flows/fr-20261021-112025-eb018fde5a0a/run.json)、[PNG](../../../autocarto/tests/golden/.runs/T53/attempt-03/survey-route/exports/考察路线.png)、[PDF](../../../autocarto/tests/golden/.runs/T53/attempt-03/survey-route/exports/考察路线.pdf)、[工具回执](../../../autocarto/tests/golden/.runs/T53/attempt-03/survey-route/tool-calls.json)、[版面预览](../../../autocarto/tests/golden/.runs/T53/attempt-03/survey-route/preview.png)。已目视检查标题、方向路线、三个站点及图例。

### 真正遇到的诊断/失败与修复

1. attempt-01 的两个新 FLOW 默认值嵌套括号，触发 `FLOW_PARSE_FAILED`；人口例还误写未支持的资源前缀“参考文档”，触发 `FLOW_RESOURCE_INVALID`。仅把新 FLOW 默认值改为平面文字、资源前缀改成规范“参考”，最终两计划无 error、无诊断。[原人口诊断](../../../autocarto/tests/golden/.runs/T53/attempt-01/province-density/diagnostics-fallback.json)、[原考察诊断](../../../autocarto/tests/golden/.runs/T53/attempt-01/survey-route/diagnostics-fallback.json)。
2. 首轮内置流程的适配器误把请求导出路径写入回执，而实际生产工具按会话归档到 `exports/`。控制器正确拒绝不存在的文件并重试，未出现伪通过。全部改用真实工具返回的 `output` 路径，初审输入同样引用真实导出结果。[原失败运行](../../../autocarto/tests/golden/.runs/T53/attempt-01/route-no-review/controller-result.json)。这不是生产工具修改。
3. 人口例最初把目标字段参数设为 `FIELD_TYPE=0`（实际为 Int），真实公式核验发现密度被取整而失败。只把动作资源中的类型改为现有 Double 值 `1`，不降低数值容差。[原失败运行](../../../autocarto/tests/golden/.runs/T53/attempt-02/province-density/controller-result.json)。
4. 考察例用点图层确定 `extent_layers` 时遇到既有边界：`_get_transformed_vector_extent` 在聚合前丢弃每个单点的空面积 bounding box，导致“没有有效的二维图层范围”。A2 验收当时未修改生产实现，只在新动作资源中使用已有显式 `extent` 接口，传真实参考底图、路线和站点的合并范围；所有站点在主图内。[原失败运行](../../../autocarto/tests/golden/.runs/T53/attempt-02/survey-route/controller-result.json)。**随后主代理另行修复了有限零面积单点包围框的聚合**，使用 3 个点的 EPSG:4326→3857 真实回归及最终完整 check 覆盖；不能把已修复边界继续列为当前缺陷，也不把该后续代码修复说成 A2 出图的前提。A2 成功回放及其动作资源保持原记录。

## 可重现命令

环境使用现有 QGIS 3.44.10 / Python 3.12.11，原外部底图与渲染器只读。输出目录必须不存在，避免覆盖旧证据。

```sh
export PYTHONDONTWRITEBYTECODE=1 QT_QPA_PLATFORM=offscreen
export AUTOCARTO_PROJECT_ROOT="$PWD" AUTOCARTO_ASSETS_ROOT="$PWD/autocarto"
export AUTOCARTO_HOME="$PWD/autocarto/tests/golden/.runs/T53/bootstrap-runtime"
export PROJ_DATA="/Applications/QGIS-LTR.app/Contents/Resources/qgis/proj"
export GDAL_DATA="/Applications/QGIS-LTR.app/Contents/Resources/qgis/gdal"
export PYTHONPATH="$PWD/autocarto/tests/golden/.runtime/deps:$PWD:/Applications/QGIS-LTR.app/Contents/Resources/qgis/python/plugins"
export AUTOCARTO_GOLDEN_BASEMAP_DIR="/Users/zw/研究生/QGIS-agent/数据/底图数据/China_map"
export AUTOCARTO_ROUTE_RENDERER_DIR="/Users/zw/Library/Application Support/QGIS/QGIS3/profiles/default/python/plugins/RouteBezierPlotPlugin"
/Applications/QGIS-LTR.app/Contents/MacOS/python autocarto/tests/integration/acceptance/run.py \
  --out autocarto/tests/golden/.runs/T53/attempt-03 \
  autocarto/tests/integration/acceptance/scenarios/route-no-review.json \
  autocarto/tests/integration/acceptance/scenarios/route-with-review.json \
  autocarto/tests/integration/acceptance/scenarios/statistical.json \
  docs/refactor/acceptance-flows/province-density/resources/acceptance.json \
  docs/refactor/acceptance-flows/survey-route/resources/acceptance.json
```

实际结果：**5/5 成功**。控制器执行时间分别为约 14.22、19.42、0.32、0.55、0.99 秒（不含每场编译、QGIS 初始化和最终预览/项目保存）。每场对 92 个原固定输入、新包资源、外部底图文件记录前后 SHA-256，全部一致。所有副作用仅在新项目下各场隔离输出和 runtime 中；原 Eino、`autocarto_data`、桌面 QGIS 工程及安装插件均未修改。

`GOPROXY=off go test ./integration/acceptance` 可编译通过；它没有 Go 单元测试，端到端证据是上述真实运行。范围内 `git diff --check` 通过。最终门禁由主代理确认 `./build.sh check` exit 0；3 项缺少 Linux 发行工件的测试跳过，live-QGIS 界面 E2E/packaged smoke 未启用，真实 LLM/视觉模型未验收，详细范围见总体 T53 记录。
