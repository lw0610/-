# v6 真实 QGIS 制图验收补充

日期：2026-10-28。独立无界面 QGIS-LTR 进程，临时 AUTOCARTO_HOME，真实工具、真实导出；不部署插件、不复用用户正在运行的 QGIS、不调用网络或在线模型。

## v6 配方迁移

- 三个原场景改为内联数据路径 `data_load_list.items` 与 `layer_load_styles.items`；每层使用资源库中的单个 QML，明确标注字段。
- 省级人口密度、考察路线两个历史示例有独立 v6 配方及自由 Markdown 验收指南；`docs/refactor/acceptance-flows` 原件不改，只读引用其输入。考察路线自带预设复制进临时工具预设目录。
- 验收桥消费 Go Library 的真实 resource handle，支持嵌套 `items.style`；不从流程解析数据表，不手造资源handle替代生产解析器。
- 工程捕获复用从“样式集”迁为单个 QML + QPT，实际走 Library.ImportNamed → GuideStore.Import → PlanExec.Build → Controller.StartFromPlan → QGIS → PNG/PDF。

## 结果与边界

**最终：5/5制图案例通过，3/3黄金对照 success=true、differences=[]；五场景输入哈希不变。捕获复用另1例通过。** [汇总摘要](qgis-summary/summary.json)。

省级人口密度、考察路线、统计专题首轮通过。路线有/无审查两场景首轮控制器均 completed，黄金对照发现禁用标注的残留配置差异（河流面标注开关仍为false）；按显式“无”清除标注配置后只重跑这两个场景。两个重跑均通过，未修改黄金基线。最终结果见 `qgis-summary/`。

捕获复用已通过，颜色 `#207f91`、`name` 标注、标题与主地图项一致；源工程与输入/导出物哈希不变。摘要：[capture-roundtrip.json](qgis-summary/capture-roundtrip.json)。

路线审查结果为明确固定的结构回执，不代表真实多模态审图质量。图件和PDF均由真实QGIS产生。黄金比较不放宽字段、未修改原基线；预览dhash只作为诊断。

## 复现

设置 `AUTOCARTO_GOLDEN_BASEMAP_DIR` 为只读 China_map、`AUTOCARTO_ROUTE_RENDERER_DIR` 为已安装 RouteBezierPlotPlugin；`PYTHONPATH` 包含仓库、golden/.runtime/deps 和 QGIS 内置 plugins。使用 `/Applications/QGIS-LTR.app/Contents/MacOS/python`，`QT_QPA_PLATFORM=offscreen`、`PYTHONDONTWRITEBYTECODE=1`。

```
python autocarto/tests/integration/acceptance/run.py --out <全新临时目录> \
  autocarto/tests/integration/acceptance/scenarios/route-no-review.json \
  autocarto/tests/integration/acceptance/scenarios/route-with-review.json \
  autocarto/tests/integration/acceptance/scenarios/statistical.json \
  autocarto/tests/integration/acceptance/scenarios/province-density.json \
  autocarto/tests/integration/acceptance/scenarios/survey-route.json
python autocarto/tests/integration/acceptance/capture_roundtrip.py --out <全新临时目录>
```

本次产物：`/private/tmp/enio2-v6-acceptance-20261028/`、`/private/tmp/enio2-v6-acceptance-routes-20261028/`、`/private/tmp/enio2-v6-capture-20261028/`。输入文件内容哈希在每个场景前后对比，摘要只保留相对产物路径。
