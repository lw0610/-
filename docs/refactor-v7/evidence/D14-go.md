# D14 执行器读回核验与取数预检补齐

2026-10-29。此轮没有启动模型任务、网络下载或修改用户会话。Python 真实 QGIS 对照见 `D14.md`、`D14-readback.json`。

## 工具调用到原修复回路

- `websocket/task_runner.go` 仅对 `layer_set_symbology`、`layer_load_style`、`layer_load_styles` 保留原始参数和完整回执。成功摘要的原有截断规则不影响这些读回；失败工具也保留回执，包括运行时错误包装中的部分批量成功结果。
- `flow_runtime.go` 将证据带入步骤结果，并合并同一步修复和进程内暂停恢复之前的样式调用。`controller.go` 把它传给已有 `VerifyOutputs`，读回不一致走现有同角色、同一步修复流程。
- `verify_symbology.go` 对齐原始参数与 QGIS 实际读回。明确传入的颜色、物理尺寸、标注字段和开关不能被工具的请求回显覆盖；auto 字段、可用字体与样式缺省值使用工具解析后的期望。
- 毫米尺寸允许差值不超过 0.01 mm；尺寸整体缩放、pt 转毫米及工具规定的字号/线宽下限与 Python 相同。符号层数量和顺序、标注配置及引用 QML 的渲染器类型也参与核验。
- 多次调用按实际图层合并。只改标注不能掩盖尚未修正的线宽；后续明确换 symbol 会将渲染器期望更新为 `QgsSingleSymbolRenderer`，不会继续要求早先 QML 的分类渲染器。

## 两处取数预检差异

1. 静态 `extent.region` 不能解析时，预检直接标为无法获取，并要求单个可识别地区、bbox 或上游范围引用。它不会再因返回了待定 extent 就把来源标成可用。合法的 `extent.from` 仍等待上游产出。
2. 预检和执行共同从已解析的版面资源读取主图框。`.layout.yaml` 按实际页边距和参照纸张缩放；`.qpt` 读取 `main_map`/`主图` 的现有矩形。四角对齐版面 A1 横向主图宽度为 714.85 mm，A4 横向为 252.45 mm，A4 纵向为 178.5 mm。自定义版面使用自己的页边距。解析得到的同一宽度送入候选查询和实际取数的比例尺估计。

## 已通过的验证

```sh
cd carto_agent
go test ./internal/flowrun ./internal/resource ./internal/websocket \
  -run 'Test(Symbology|StyleReadback|ReferencedStyle|FailedStyle|ControllerRepairsSymbology|LayoutMapWidth|V7PrecheckRejects|V7PrecheckAndExecution)' \
  -count=1
```

三个包通过。覆盖：

- 7 组真实 QGIS 图层的 A1/A4 回执；正确请求接受，错误颜色、线宽、字段、开关、合并属性和渲染器拒绝。
- 0.01 mm 误差接受，0.011 mm 拒绝；请求回显被错误改写时仍以原调用核验。
- 部分失败批量工具经真实错误包装后仍保留成功行与失败原因；引用 QML 类型和关闭标注核验。
- 真实 Controller 路径第一次读回不符、第二次同角色修复成功；首次失败验证与后续成功验证均留在步骤尝试中。
- 加载分类 QML 后主动换为参数符号，新的渲染器被正确核验；仅修标注不会清除符号未修复项。
- 静态组合地区拒绝；上游产出范围仍按依赖执行。
- 系统 YAML、自定义 YAML、QPT 主图框宽度；预检保存的查询宽度与执行使用的资源宽度一致，比例尺使用该主图框计算。

这些结果证明局部契约和执行器接线，不替代整项目门禁或真实模型端到端制图验收。
