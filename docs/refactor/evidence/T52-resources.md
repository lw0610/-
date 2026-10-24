# T52：旧资源包及 Python 兼容入口下线

## 范围与前置验证

仅在 `enio2` 实施本子范围。T51 已确认默认及五个开发方案完成 v2 迁移、原始备份与密钥未变化，随后才删除旧目录。不改原 Eino、开发运行数据、Go 配置/运行时、前端或 removal ledger 主体。Go 旧工作流删除、实际 catalog/authority 验证及最终构建门禁由另一子任务和主代理负责。

## 实际删除与资源保留

- 删除 57 个并入、流程、资源、删除类技能根目录，包含 `misc-tools`、`layout-image-export`。完整名称固定记录在 `autocarto/tests/fixtures/retired_skill_packages.json`；测试逐项验证不存在，剩余一级包均为 `kind: category`。`docs/refactor/scene_word_debt.txt` 为空，场景守卫通过。
- `china-admin-boundaries` 的 11 个数据/许可/溯源/选择器/测试文件整体移动到 `autocarto/resources/system/data/中国行政区边界/`，保留 `data/` 子层，来源 manifest 的相对路径、字节数、哈希和上游文件不变。三份完整原始面数据为 34 省、375 市、2891 县，共 3300 要素。测试核对全部资产来源哈希及全部多边形有效性，并实际导出单面、读取 GEE worker 的区域校验器；没有网络请求。
- T40 行政区工具只读取上述资源库，不再向旧技能目录回退。测试保留一份旧路径夹具并确认其不能被使用，再检查新资源、篡改和身份缺失的拒绝行为。GEE 的来源说明改为当前资源和正式工具包位置；其模板没有旧本地路径引用。随资源移动的独立选择器记录 `resource` 与上游技能身份，保留来源版本。
- GEE 已迁移的许可、三份完整参考、两个模板保持逐字节一致；删除旧副本前固定六个文件的 SHA-256 到 `autocarto/tests/fixtures/gee_licensed_assets.json`。测试直接校验目标文件和固定哈希，避免旧目录删除后遍历为空却误报通过。`UPSTREAM.md` 保留，并明确更新行政区资源位置。
- 删除 `cartography_style_impl.py`，生产 `cartography/handlers/style.py` 按 basic、thematic、labeling、basemap、mapping 实际模块导入；补齐父模块中实际缺失的 mapping 导出。相关样式、专题、统计、图文测试及黄金驱动均直接导入实际实现。
- 删除路线组合 YAML、`cartography_build_and_draw_from_tables_handler` 及正式包引用。旧组合只是三个独立工具的串行参数/回包适配；黄金 legacy 驱动改为直接调用路线、点和图文标注，显式保留旧场景参数和原手动排版。新生产流程继续使用三个独立入口。旧回包形状断言替换为真实 manifest 调用后实际点数量/图层身份/注记数量及旧入口不可发现的断言。
- 删除 `layout_add_file_overview` YAML、专用模块和 handler 导出。调用者综述由 `layout_compose` 使用。真实 QGIS 测试在禁止 Python `open` 的条件下传入完整正文，核对换行、HTML 转义及省略综述行为；不再为旧入口保留单元夹具。
- 删除 `canvas-navigation/config.yaml` 中唯一的 `enabled: false` 旧覆盖。其正式头部 `enabled: true` 成为唯一声明，不修改业务权限。实际 Go catalog/authority 验证由 T52 Go 子范围完成，不能以本 Python 元数据断言替代。

## 旧测试保护点的替代依据

| 旧保护点 | 现在的验证 |
| --- | --- |
| 旧流程/方法包保留及 SKILL 提示词固定句子 | 57 项目录退出、正式 category 归属、方法引用真实文件、体积预算、四内置流程真实编译/规则验证（T50） |
| GEE 旧目录与新目录相等 | 六文件固定哈希、许可/来源文件存在、独立边界/GEE reader 的实际读入 |
| 样式转导出仍可调用 | 实际模块的符号/renderer/labeling/像素等价，真实分级设色和导图 |
| 路线组合私有 receipt 形状 | 独立工具实际 authority manifest、81 点、2 注记、六表及资源优先级、真实路线渲染、四组黄金回放 |
| 比例尺代码包含 FitWidth/主图/投影/尺寸字符串 | 独立真实 QGIS 断言 FitWidth、60–80 mm 预算、同一可见 linkedMap、主图 CRS/范围不变、公里单位、实际 resize 回执、地图框范围比例、网格默认工程 CRS；既有真实几何变换范围测试继续保留 |
| CRS 提示词要求不强制投影 | 重投影工具真实算法/参数结构，以及既有执行器的输入 CRS、目标 CRS、输出 CRS/要素数守卫；实际制图范围变换和主图 CRS 保留测试 |
| QPT YAML/导出代码包含某字符串 | 解析工具定义和参数映射，T45 真实 manifest 调用、占位符替换/未替换提示、图层/范围、失败保留旧版面 |
| 质量 SKILL 包含八维和固定保护句 | Python 保留实际原图、图片格式/大小、快照、锁定内容、合法锚点、CLI 回执；初终审八维/建议执行回执由 flowrun 的实际审查校验及 T50 计划测试覆盖。没有恢复候选图片功能或固定 DPI |

新增 `test_retired_tools.py` 解析全部工具定义及正式归属，验证两个旧工具无定义/归属、两个旧模块不可导入、相关 handler 导出不存在。工具孤儿白名单改为空。

## 真实执行记录

工作目录为新项目根。使用现有 QGIS 3.44.10/Python 3.12.11，不安装组件。每次明确指定新项目根、新 assets 和独立临时 runtime；不继承 shell 指向旧项目的配置。

```sh
export PYTHONDONTWRITEBYTECODE=1 QT_QPA_PLATFORM=offscreen
export AUTOCARTO_PROJECT_ROOT="$PWD" AUTOCARTO_ASSETS_ROOT="$PWD/autocarto"
export AUTOCARTO_HOME="$(mktemp -d /tmp/enio2-t52-final.XXXXXX)"
export PYTHONPATH="$PWD/autocarto/tests/golden/.runtime/deps:$PWD:/Applications/QGIS-LTR.app/Contents/Resources/qgis/python/plugins"
export PROJ_DATA="/Applications/QGIS-LTR.app/Contents/Resources/qgis/proj"
export GDAL_DATA="/Applications/QGIS-LTR.app/Contents/Resources/qgis/gdal"
export AUTOCARTO_GOLDEN_BASEMAP_DIR="/Users/zw/研究生/QGIS-agent/数据/底图数据/China_map"
export AUTOCARTO_ROUTE_RENDERER_DIR="/Users/zw/Library/Application Support/QGIS/QGIS3/profiles/default/python/plugins/RouteBezierPlotPlugin"
QPY=/Applications/QGIS-LTR.app/Contents/MacOS/python
```

| 实际命令（均带上述环境） | 结果 |
| --- | --- |
| `"$QPY" -m pytest autocarto/tests/test_tool_package_methods.py autocarto/tests/test_scalebar_layout_contract.py autocarto/tests/test_admin_boundary.py autocarto/tests/test_retired_tools.py autocarto/tests/test_definition_validation.py autocarto/tests/test_handler_tool_package_migration.py autocarto/tests/test_crs_reprojection_guardrails.py autocarto/tests/test_import_layout_template.py autocarto/tests/test_quality_feedback_review.py autocarto/resources/system/data/中国行政区边界/tests/test_select_boundary.py autocarto/tests/test_scene_word_guard.py -q` | **71 passed，3785 subtests passed**，7.08 秒 |
| `"$QPY" -m pytest autocarto/tests/test_layout_refactor_qgis.py autocarto/tests/test_layout_compose_qgis.py autocarto/tests/test_import_layout_template_qgis.py -q` | **19 passed**，20.70 秒 |
| 独立进程分别运行 `test_statistical_layout_qgis.py` / `test_thematic_style_qgis.py` / `test_callout_rendering_qgis.py` | **6 / 5 / 4 passed**，7 / 12 / 5 个 subtests，0.81 / 0.56 / 0.83 秒 |
| 独立进程运行 `test_style_set_qgis.py` / `test_data_loading_qgis.py` | **30 / 10 passed**，1.43 / 1.55 秒 |
| `"$QPY" -m pytest autocarto/tests/test_route_tools_qgis.py -q` | **10 passed**，11.67 秒；真实外部路线渲染器已使用，只有 QgsField 弃用提示 |
| `"$QPY" -m pytest autocarto/tests/golden -q` | **10 passed**，35.71 秒；legacy/new × 路线/统计全部真实回放，完整结构比较无差异，标题加粗/锁定/中心锚点独立断言通过 |
| `git diff --check -- autocarto` | 通过 |

### 过程中的真实失败与修正

首次迁移测试发现 selector 的仓库根层数随目录移动变化，以及新 runtime 包允许省略 `enabled`（默认启用），修正对应测试定位/默认值断言。综述测试第一次误把未指定 heading 的纯文本模式断言成 HTML，后显式提供 heading 和宽度预算，实际完整文字检查通过。

QGIS wrapper 的 Python 单独读取 GeoPandas 时未自动提供 PROJ/GDAL 数据路径；第一次 GEE reader 验证因此失败，明确提供应用内现有数据目录后通过，没有改变生产校验器或跳过实际读入。

曾把固定统计夹具 provenance 的历史路径更新为当前路径，黄金比较正确报告唯一的 fixture 哈希变化，四组几何/渲染比较当时已无差异。已恢复原夹具字节及 SHA-256 `90cdb22198224b8a0372df43ba750e08885f41f731338c8e139a5950ba57ab8d`，新位置说明放到黄金 README；最终四组比较全部通过。没有修改任何黄金基线、固定输入、比较容差或忽略字段。

## 交给主代理的合并信息与边界

本子范围满足 ledger 的 `python_cartography_style_module_reexports` 和 `python_route_combined_handler_compat` 实际删除条件；可合并至 completed removals 并引用本证据。旧 v1 reader 的历史配置/迁移夹具仍由 Go 子范围按其兼容窗口处理，本次不按文本命中机械删除。

这里的黄金回放验证独立工具和版式结果，**不是** Controller 端到端流程执行证明；后者属于 T53。完整 Python 集合、Go 门禁和 `./build.sh check` 的最终结果由主代理单独汇总，不能从本文件的针对性通过推导为全部通过。
