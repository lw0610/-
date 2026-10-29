# D50 后端守卫与真实 QGIS 离线回归

## 结论
五个确定性角色适配器场景均完成真实 QGIS 制图/导出。两条路线与统计场景的黄金结构对照零差异，所有输入数据哈希不变。模型调用为 0；真实在线模型、在线取数和视觉审查效果不在此结论内，归 D51。

## 改动摘要
- 移除验收脚本通过 `AUTOCARTO_GOLDEN_BASEMAP_DIR` 直接喂步骤数据的捷径。显式 `--layers` 只供导入：真实扫描→文件头检查→QGIS 元数据/CRS 探测→清单→预览→名称匹配→计划预检→控制器逐项准备→`layer_load_builtin` 加载。
- 适配器生成真正的 carto-plan/v2：`source/ref`、`inputs/produces`，中文显示名与 ASCII 产物 ID 分开；不再携带 find_id/choice/uses/outputs。
- 原有黄金对照保留。导入完成后再读取流程，使锁定的八个用户图层与实际匹配结果一致。
- 添加守卫：验收计划不含旧字段，路线必须使用登记加载且保留黄金比较。
- 四个 WebSocket 测试文件迁移：当前用户/会话引用按 v2；旧冻结查找/派生范围/HITL 恢复明确用 LegacyAPIVersion+prepareLegacyPlan 构造持久化夹具，不通过新计划入口提交 v1。
- `prepare_v7_test.go` 覆盖并行一项失败时保存成功同伴输入与双方台账、用到之前才获取、预算暂停后取消落盘、纯表格步骤不依赖 QGIS、QGIS 快照失败后暂停。

## 运行命令
在项目根目录，使用 QGIS 自带解释器及其 Python/QGIS 路径，测试依赖沿用仓库已有 `.runtime/deps`：

```sh
AUTOCARTO_ROUTE_RENDERER_DIR='/Users/zw/Library/Application Support/QGIS/QGIS3/profiles/default/python/plugins/RouteBezierPlotPlugin' \
PYTHONPATH='.:autocarto/tests/golden/.runtime/deps:/Applications/QGIS-LTR.app/Contents/Resources/python3.12:/Applications/QGIS-LTR.app/Contents/Resources/python3.12/lib-dynload:/Applications/QGIS-LTR.app/Contents/Resources/python3.12/site-packages:/Applications/QGIS-LTR.app/Contents/Resources/qgis/python' \
/Applications/QGIS-LTR.app/Contents/MacOS/python autocarto/tests/integration/acceptance/run.py \
  autocarto/tests/integration/acceptance/scenarios/route-no-review.json \
  autocarto/tests/integration/acceptance/scenarios/route-with-review.json \
  autocarto/tests/integration/acceptance/scenarios/statistical.json \
  autocarto/tests/integration/acceptance/scenarios/province-density.json \
  autocarto/tests/integration/acceptance/scenarios/survey-route.json \
  --layers '/Users/zw/研究生/QGIS-agent/数据/底图数据/China_map' \
  --out /tmp/autocarto-v7-acceptance-five-final-20261029 \
  --python /Applications/QGIS-LTR.app/Contents/MacOS/python
```

输出目录必须尚不存在。之后把流程读取顺序移到图层导入之后，针对受影响的两路线在 `/tmp/autocarto-v7-acceptance-routes-lock-final-20261029` 再次通过，锁里均为八个真实导入的 `layer.user.*`。

## 计数与耗时
下表执行时间为控制器 started_at→finished_at，不含编译、导入、预览；此时其他门禁并行运行，不能据此估算真实模型任务速度。

| 场景 | 状态 | 模型调用 | QGIS 工具调用 | 控制器耗时 | 数据准备项 | 黄金结构差异 |
|---|---|---:|---:|---:|---:|---:|
| 行军路线，无审查 | completed | 0 | 48 | 37.76 秒 | 9 | 0 |
| 行军路线，有审查 | completed | 0 | 59 | 36.45 秒 | 9 | 0 |
| 统计专题图 | completed | 0 | 19 | 0.38 秒 | 2 | 0 |
| 省域密度图 | completed | 0 | 23 | 0.57 秒 | 0 | 无黄金基线，实际字段/公式/版面验证 |
| 调查路线图 | completed | 0 | 19 | 1.04 秒 | 1 | 无黄金基线，真实数据/工具/版面验证 |

完整精简结果见 `D50-backend-summary.json`。每个输出目录保留 plan、controller-result、events.jsonl、transcript.jsonl、tool-calls、数据台账、原件哈希、PDF/PNG、project.qgz。

## 本轮验证实际发现并修复的问题
1. `layer_load_builtin` 工具定义存在但 data-loading 手册未授权，真实角色执行被拒绝；已在同一工具包授权列表补齐。
2. 新增图层清理误删版面图例引用的辅助层，黄金快照发现 `node.layer()==nil`。控制器仅传本步成功产出的版面名，隐藏清理工具保留这些版面地图/图例确实引用的层；失败仍清除本次新建层。不扩大成“保留所有新层”。真实 QGIS 回归覆盖并通过。
3. 验收在导入前读流程导致锁引用旧系统别名；已更正顺序并重跑两路线。

## 其他测试
- `go test ./internal/websocket -run 'TestPlanV2KeepsDistinct|TestCartographicPlanReusesRegisteredDataset|TestSessionPlanReference|TestLegacyDerived|TestLegacyPlanningAcquisition' -count=1`：通过，4.64 秒。
- `go test ./internal/flowrun -run '^TestV7' -count=1`：5 项通过，0.66 秒。
- `go test -race ./internal/flowrun -run '^TestV7' -count=1`：通过，1.80 秒。
- 定向 `layers/flow/api/integration/acceptance`：全部通过。
- 整体 `./build.sh check` 及其他包的 race 结果由总门禁记录，不用上述子集替代。

## 图像复核
已打开查看 `D50-route-preview.png`：路线、真实底图、图例、注记、标题与比例尺可见；截图仅证明固定样例渲染，不证明模型规划或任意地图质量。

## 与方案的边界
离线适配器复用生产清单/匹配/预检/计划构建/控制器和真实工具；模型角色与审查意见仍为明确的确定性夹具。数据准备回调在验收入口中组装，不能宣称等同于真实会话的在线取数智能体。真实模型回放由 D51 单独承担。

## D31 追加回归：预算、同智能体修复、计划状态

- 工具限额为 2 时，第三次调用被拒绝后整体暂停；显式继续可完成、取消可落盘，成功领取的工具计数与实际执行次数一致。测试发现旧 `Take` 把被拒绝的调用也计入，现用原子领取，仅领取成功计数。
- 真实生产 Plan→Controller→ADK 流程测试第一次写出缺少 `value` 字段的 CSV，内容检查失败；第二次的消息保留第一次 assistant 工具调用、工具结果、回执，并追加“仅修正”与缺字段原因，仍由同一 Agent 实例执行。BuildRole 总计 1 次，执行 2 次，最终表格通过核验。
- 修复数据项的 `completed/pending_agent/failed` 被当成整个计划状态的问题。仅运行生命周期事件修改计划快照；四种数据准备事件保留原始 flow_event 且不写计划文件。等待、暂停、重试规划和整体完成的状态转换均有回归。
- `go test -race ./internal/websocket ./internal/flowrun -run '^TestV7(ContentRepair|DataPreparationEvent|ToolBudget)' -count=1`：通过，两个包分别约 3.42 / 2.08 秒。
- 单阶段预算与显式继续的计数已验证；跨修复累计预算由控制器主任务继续实施与验证。

## 2026-10-29 追加：D14 新参数工具经过真实控制器

新版 `06` 修改后重新跑五个固定角色/真实 QGIS 场景，并补充 A4 行军图。五个场景均完成，另一个 A4 变体也完成；模型调用和联网调用均为 0，输入文件哈希保持不变。摘要见 `D50-symbology-summary.json`。这覆盖真实图层导入、资源解析、计划 v2、控制器、QGIS、文件导出，不证明在线模型总能正确翻译流程。

### 场景与验收改动

- 两个行军场景的符号化步骤改为七层 `layer_set_symbology`，仅铁路用 `layer_load_style`；A1 传 `scale=1`，A4 传 `scale=0.35`。参数逐项来自新版流程表，省名包含旧 QML 的白色 0.7 毫米描边，标注字段使用 `auto`。
- 每次符号调用后，适配器重新读取真实 QGIS 图层设置，对照规范化后的期望，不只接受工具返回成功。路线场景保存八项 `symbology-readback.json`，A1/A4 均零差异。
- 按新版图层表，地区界线移至省域之上、水系之下。原 v6 黄金快照的顺序是地区界线在铁路之上，差异单独保留在 `comparison.json.v6_differences`；新版顺序单独严格断言。没有改原黄金文件，也未忽略其他渲染、标注、数据、版面和成果结构差异。两场 A1 路线仅这一条预期顺序变化，其余结构零差异。
- 五场首轮中，三场非行军成功；两场行军在导入预览时发现验收 Go 适配器仍把默认样式名直接传给 QGIS，违反现有资源句柄接口。修正验收入口：通过实际 `Library.Resolve` 解析默认样式，预览与随后内置图层加载消费同一真实句柄；没有绕过资源校验或改生产守卫。修后两场行军重跑成功。
- 已删除废弃的 `legacy-style-presets/SKILL.md`，它仍引用本轮删除的三个类型预设工具；默认方案和用户配置无该包引用。新工具归现有符号与标注包，不新增工具包。

### 输出与耗时

| 场景 | 控制器耗时 | 工具调用 | 结果目录 |
|---|---:|---:|---|
| route-no-review | 13.775 秒 | 49 | `/tmp/autocarto-v7-d14-acceptance-routes-20261029/route-no-review` |
| route-with-review | 17.695 秒 | 60 | `/tmp/autocarto-v7-d14-acceptance-routes-20261029/route-with-review` |
| statistical | 0.385 秒 | 17 | `/tmp/autocarto-v7-d14-acceptance-five-20261029/statistical` |
| province-density | 0.638 秒 | 21 | `/tmp/autocarto-v7-d14-acceptance-five-20261029/province-density` |
| survey-route | 1.288 秒 | 19 | `/tmp/autocarto-v7-d14-acceptance-five-20261029/survey-route` |
| route-no-review-a4 | 11.897 秒 | 49 | `/tmp/autocarto-v7-d14-acceptance-a4-20261029/route-no-review-a4` |

耗时不含 Go 构建、图层导入、预览与最后黄金快照；不是在线模型任务耗时。目录保留完整 `scenario.json`、`carto-plan.json`、`plan.json`、数据预检、`controller-result.json`、事件、工具调用、原件哈希、PNG/PDF 与 QGIS 工程。

A4 变体由当前 `route-no-review.json` 仅修改场景编号、纸张 A4、两次符号调用 `scale=0.35`、移除 A1 黄金布局对照生成，固定路线数据不变。实际页面为 297×210 毫米；省名 1.82 毫米、河名和驻地名 1.8 毫米、河流面边线 0.1 毫米、驻地点外/内圈 0.63/0.28 毫米、铁路虚线间隔 1.4/1.4 毫米。中文字体实际为 PingFang SC。没有 Windows 实机运行证据，跨平台字体行为另由 D14 检查。

### 运行与复核

命令沿用本记录上面的 QGIS Python、PYTHONPATH、只读 `--layers` 参数；两场路线输出为 `/tmp/autocarto-v7-d14-acceptance-routes-20261029`，A4 使用 `/tmp/autocarto-v7-d14-route-a4.json`，输出为 `/tmp/autocarto-v7-d14-acceptance-a4-20261029`。完整已展开配方均在各场景的 `scenario.json`。

- `go test ./internal/skill -run '^TestToolsBelongToToolkits$' -count=1`：通过，0.408 秒。
- `go test ./integration/acceptance -count=1`：通过，0.544 秒。现有守卫同时检查真实登记加载、保留黄金比较、调用参数工具，避免退回八层套样式。
- 已打开 A1/A4 真实预览，中文、路线、底图、图文注记、图例和版面可见。A4 的图文注记与装饰沿用旧版面尺寸，仍显得偏大，底图缩放通过不代表所有布局元素均已优化；本轮不扩大到图文注记排版修改。
- 最终像素量化见 [D14](D14.md) 与 [D14-pixel-metrics.json](D14-pixel-metrics.json)：修正省名属性后，两场完整A1图相对v6实际参考均变化3.5913%（144,032像素），MAE为1.63949、RMSE为12.01311、dHash距离为2；相对仓库原金样变化3.6968%。7层在统一中文字体及标注策略后受控对照为0像素差异。既有门禁没有预设整图像素阈值，故正式整图像素验收仍为“未判定通过”，不能据这些诊断数字声称在阈值内通过。

## 追加：省名粗体与避让属性修正后的两路线复验

实际像素对照发现省域旧QML使用粗体字重75、`obstacle=false`，此前参数未显式保留。流程和两个场景已补 `labels.bold:true`、`labels.obstacle:false`；障碍属性表示省域面要素不参与其他标注避让，不表示文字可以任意重叠。参数工具补齐后重新构建验收程序，再跑两个固定路线场景，未覆盖原证据目录。

新输出根目录：`/tmp/autocarto-v7-d14-acceptance-labels-final-20261029`。

| 场景 | 状态 | 控制器耗时 | 工具调用 | 独立读回 | 参数差异 |
|---|---|---:|---:|---:|---:|
| route-no-review | completed | 16.221秒 | 49 | 8项 | 0 |
| route-with-review | completed | 21.817秒 | 60 | 8项 | 0 |

两个场景的 `symbology-readback.json` 均实际读到省名 `bold:true`、省域面 `obstacle:false`，同时保持5.2毫米、PingFang SC、白色0.7毫米描边和每省显示。原始输入哈希不变，模型/联网调用仍为0；新版结构差异为0，相对原v6结构仍只保留已明确的图层顺序变化。新增定量像素结果由D14独立记录，本次8项参数读回不代替该结果。

这次仅复验受新增标注属性影响的两条A1路线。此前A4场景验证的是尺寸缩放；本次没有重新跑A4全图，新增属性的A4工具读回由D14对应测试覆盖。流程锁刷新、flow/api/acceptance包和12项Python流程契约检查通过，命令与耗时见D40末尾。

## 公开工具派发统一后的本地复验

在统一 Go 公开工具名与 Python 原生算法解析、最近邻复用通用 Processing 执行器之后，五个既有固定场景再次完成。没有调用在线模型或在线取数，输入文件哈希不变。

| 场景 | 状态 | 控制器耗时 |
|---|---|---:|
| route-no-review | completed | 14.606秒 |
| route-with-review | completed | 20.607秒 |
| statistical | completed | 0.592秒 |
| province-density | completed | 0.627秒 |
| survey-route | completed | 1.178秒 |

结果目录：`/tmp/autocarto-v7-final-processing-20261029`；命令日志：`/tmp/enio2-v7-final-processing-scenarios.log`。这些仍是固定角色的本地真实 QGIS 检查，不是 D51 真实模型验收。后续版面警告接线只做对应模块检查，不再重复这五个制图场景。
