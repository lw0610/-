# V51 工程捕获资源完整复用链

## 结论
通过。已用真实无界面 QGIS 从工程导出 QPT 和样式集，使用生产 Go 资源库导入、导入一个新流程、编译并由真实运行控制器执行，在新的 QGIS 进程消费资源句柄，生成 PNG 和 PDF。原始数据和导出资源字节保持不变。这里证明的是离线可复现程序链；不把它描述为真实在线模型或实时桌面操作。

## 改动摘要
- `autocarto/tests/integration/acceptance/capture_roundtrip.py` 新增隔离验收场景。全部数据、资源、流程、方案、缓存、输入记忆和运行记录写入调用者指定的全新输出目录。
- `carto_agent/integration/acceptance/main.go` 新增通用可选字段 `resource_imports`（`source/type/name`）和 `flow_package`，分别调用真实 `Library.ImportNamed` 与 `Store.Import`。结果保存在 `imported-resources.json`。原五场景不提供这些字段，路径保持不变。
- 同一适配器内 V42 的必要改动一并提交：删除旧目录绑定调用；基础体检走 `BasePlan`；测试固定模型显式调用 `Compile`；行军底图由运行输入传入。V42 五项实际执行已在其证据中记录。
- 没有新增产品兼容分支，也没有修改黄金基线。

## 实际链路和断言
1. 在独立 QGIS 进程里创建一份仅用于验收的区域 GeoJSON、带字段标注和指定颜色的图层、含 `{{标题}}` 文本的版面。按真实 YAML 工具定义，通过 `ProcessingToolExecutor` 调用公开的 `layout_save_template`、`style_set_export`。
2. 验证导出后的源工程仍只有原版面，源地图 ID 为 `original_map`、标题仍是占位符且脏状态未变；QPT 副本的主地图 ID 为 `main_map`；样式集返回的简单标注字段为 `name`。
3. Go 适配器真实导入两类资源，生成新身份和清单；实际导入新 FLOW，由真实编译器按公共声明解析并写入 `flow.lock`。断言库项、计划、锁定文件的身份和摘要一致，所有者为 `library_user`。
4. 真实 `Controller.Start` 运行四步，固定角色动作调用实际 QGIS 工具加载数据、应用样式集、导入 QPT、导出图片和 PDF。工具参数中的资源句柄来自真实 Go `StepRequest.Resources`，断言句柄指向隔离资源库副本，身份/摘要与导入结果一致。
5. 实际结果核对：符号颜色 `#207f91`；标注启用且字段为 `name`；主地图只有验收图层；标题替换为“工程模板复用验收”，未替换占位符为空；主地图 ID 为 `main_map`；PNG/PDF 文件头正确且非空。版面几何、标题字重和预览通过现有实际 QGIS 快照逻辑检查。人工查看 PNG，标题、区域、甲区标注可见。
6. 前后 SHA-256 比对通过：源 GeoJSON、QPT、CSV、QML 均未变化；现有验收输入和黄金夹具哈希也未变化。

## 运行命令与结果

```sh
PYTHONPATH="$PWD/autocarto/tests/golden/.runtime/deps:$PWD:/Applications/QGIS-LTR.app/Contents/Resources/qgis/python/plugins" \
PROJ_DATA=/Applications/QGIS-LTR.app/Contents/Resources/qgis/proj \
GDAL_DATA=/Applications/QGIS-LTR.app/Contents/Resources/qgis/gdal \
QT_QPA_PLATFORM=offscreen \
/Applications/QGIS-LTR.app/Contents/MacOS/python \
  autocarto/tests/integration/acceptance/capture_roundtrip.py \
  --out /tmp/enio2-v51-capture-final-20261023
```

从 enio2 根目录执行；脚本强制设置 enio2 project/assets、输出目录内 runtime、禁用 keyring。没有读取或改写真实 `autocarto_data/`，没有连接用户当前 QGIS 工程。

- `go test ./integration/acceptance -run '^$'`：通过，适配器可编译。
- 完整命令 exit 0；Controller 场景 success=true、inputs_unchanged=true；最终 roundtrip-report success=true、source_project_unchanged=true、source_files_unchanged=true。
- 最终补充的资源句柄身份/摘要/根目录断言使用已保留的完整执行产物重新验证，通过；无需重复运行 QGIS。
- `git diff --check`：通过。
- 第一轮工具链和 Controller 已通过，但验收脚本错误假定导出文件直接在运行根目录、库所有者在 `source` 字段；已按真实输出路径和 `owner` 契约修正，在全新目录重新执行通过。没有因此放宽产品断言。

## 产物

- 总日志：`/tmp/enio2-v51-capture-final.log`
- 最终报告：`/tmp/enio2-v51-capture-final-20261023/roundtrip-report.json`
- 原工具调用结果：同目录 `capture-results.json`
- 实际新流程：同目录 `new-flow/FLOW.md`
- 实际运行：同目录 `run/capture-roundtrip/`
- 运行目录保留 `plan.json`、`imported-resources.json`、`controller-runtime/flows/captured-project-roundtrip/flow.lock`、运行状态、事件、协议记录、`tool-calls.json`、`snapshot.json`、`compose_validation.json` 和 `project.qgz`。
- 成果：运行目录 `exports/捕获复用.png` 与 `exports/捕获复用.pdf`；macOS 解析后的绝对路径为 `/private/tmp/...`。

## 跳过的测试及原因
- 实时捕获 HTTP 的鉴权、权限、参数、返回、暂存隔离仍由 V32 的连接模拟器测试覆盖；本场景直接走工具导出与生产库导入，不声称执行了活的 HTTP 传输。
- 实时桌面点击和前端 UI 由前端测试覆盖，本场景没有控制用户桌面。
- 模型使用确定性适配器，无联网推理；结果不证明模型自主工具选择与自然语言规划准确性。
- V42 已真实通过的原五项制图验收复用其产物，不因新增可选验收入口重复运行。
- 完整构建检查由 root 统一执行，本卡不并行启动重型全量构建。

## 与方案不一致之处
采用经过批准的最小隔离链：真实 PyQGIS 导出 → 生产资源库导入 → 新流程引用 → 真实控制器和 QGIS 执行。实时 HTTP/交互 UI 与在线模型属于独立覆盖边界，未伪装为已实测。

## 修改了涉及范围以外的文件
无。离线验收适配器属于 V51 验收基础设施；同文件 V42 输入契约改动在上文明确登记。

## 遗留问题与后续任务需要知道的事
本场景无未通过的程序验收项。整体 G1/G3 边界仍以主验收记录为准；上线前若需要宣称真实在线模型或实时桌面体验已验收，必须另做对应测试。
