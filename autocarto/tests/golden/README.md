# 黄金样例回归

这是 T01 的固定输入和真实 QGIS handler 回放脚手架。每次运行新建独立 profile、工程、runtime 和输出目录；不连接桌面 QGIS，不读写用户正在打开的工程，不修改 `autocarto_data/`。只允许向新的输出目录生成成果，已有 `snapshot.json` 时拒绝覆盖。

## 固定输入

- `fixtures/route_dataset/`：只读复制自校验成功的会话 `5b9b9879-9968-444d-97ea-1f2eea706a5d` 的六表、符号和两条注记图片。81 个地点、16 条路线、100 个路径节点、81 个专题点、5 个符号配置、2 条注记。图片绝对路径改为夹具内相对路径；`fixture_provenance.json` 记录原始/夹具哈希与改写，不提交用户原文或整个运行目录。
- `fixtures/statistical/`：随项目地级市面数据中 adcode 以 42 开头的 17 个面，按 adcode 排序；数值为明确标注的合成数据，含有效零值、一个空值和一个没有对应面的地区。夹具 provenance 保留生成时的历史路径及哈希；当前同字节原始数据位于 `autocarto/resources/system/data/中国行政区边界/data/`。驱动先验证未匹配报告，再显式 `allow_unmatched=true` 生成派生文件，缺值保持 null。

## 环境和运行

需要 QGIS Python、Processing、Pillow 和 pytest。普通 Python 没有 QGIS 时，纯逻辑测试仍可运行，真实场景测试会明确跳过。macOS 使用 QGIS 应用的 `Contents/MacOS/python` wrapper，以便正确设置 Python 运行库；其他系统使用已经能导入 QGIS 的 Python。

路线场景需要两个外部目录，不会将这些数据或插件复制到仓库：

```sh
export AUTOCARTO_GOLDEN_BASEMAP_DIR="<底图目录>"
export AUTOCARTO_ROUTE_RENDERER_DIR="<含 services/combined_plot_service.py 的渲染插件目录>"
```

```sh
python autocarto/tests/golden/run_golden.py --scenario statistical --driver legacy --out autocarto/tests/golden/.runs/statistical
python autocarto/tests/golden/run_golden.py --scenario route --driver legacy --out autocarto/tests/golden/.runs/route
python autocarto/tests/golden/compare_golden.py autocarto/tests/golden/baseline/statistical autocarto/tests/golden/.runs/statistical
python -m pytest autocarto/tests/golden -v
```

`run_golden.py` 缺 QGIS 或缺路线环境时返回 77；真实执行失败返回 1。T44 已启用 `--driver new` 的真实 `layout_compose` 排版，并输出原始 `compose_result.json` 与独立的标题字重/锚点验证。数据、样式和路线阶段已按 T40/T41/T43 接入独立工具。T52 删除旧组合 handler 和样式转导出后，legacy 驱动也直接调用独立路线、点、注记及各样式实现，保留原参数、手动排版和原基线。

T01 旧驱动没有设置标题加粗，与旧技能说明及新规范不一致。新驱动按规范加粗，保留旧基线不改；当前实际标题宽度/位置变化也在原 1 mm 容差内，因此全量结构比较不添加忽略项、不放宽容差。`compose_validation.json` 另要求标题实际加粗且锚点误差不超过 1 mm。黄金路线显式关闭旧场景没有的网格；新工具默认稀疏网格由单独 QGIS 测试覆盖。

```sh
python autocarto/tests/golden/run_golden.py --scenario statistical --driver new --out autocarto/tests/golden/.runs/statistical-new
python autocarto/tests/golden/compare_golden.py autocarto/tests/golden/baseline/statistical autocarto/tests/golden/.runs/statistical-new
```

QGIS Python 未带 pytest 时，可在本目录的忽略目录安装测试依赖，不修改 QGIS 应用：

```sh
python -m pip install --target autocarto/tests/golden/.runtime/deps pytest
PYTHONPATH="$PWD/autocarto/tests/golden/.runtime/deps:$PWD" python -m pytest autocarto/tests/golden -v
```

## 输出与基线

每个场景生成 `snapshot.json` 和 72 DPI 的 `preview.png`。快照包括元素 ID/类型/矩形/字号、主图图层顺序和范围、图例条目、渲染器类型/dump/哈希、标注字段、固定输入哈希、QGIS 版本。哈希中的资源根目录转换为稳定占位符，避免运行目录或安装路径不同造成误报。快照不记录随机 QGIS 图层 ID。

legacy 路线回放直接使用独立工具并在测试代码确定性计算 A1 横向字号、四角锚点、装饰框外缘 5 mm 和比例尺目标条带 80 mm；比例尺通过真实尺寸及参考元素对齐。统计回放按旧统计流程创建主图、五级分位数图例、缺测底色和数据说明。没有调用大模型或网络服务。

`baseline/{route,statistical}/` 只能放实际运行 legacy 产生的快照和预览；生成环境及命令见 `docs/refactor/evidence/T01.md`。修改固定输入或基线必须有明确原因、重新运行记录，不能为使比较通过直接改期望值。

比较默认检查全部结构字段；矩形误差容许 1 mm，字号、图层顺序、图例、渲染器哈希必须一致。预览使用 64 位差分感知哈希，只报告距离，不决定通过与否。可传 `--config` 指定 JSON，例如 `{"ignore":["layers/*/renderer_sha256"],"tolerance_mm":1.0}`；任何忽略项都应在任务证据中说明原因。不同 QGIS/字体/外部渲染插件版本可能产生真实差异，不自动放宽比较。
