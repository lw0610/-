# D31 当前版面警告接线

2026-10-29。方案 `03` §5.4 明确要求版面存在且越界/重叠作为核验问题。此前 Go `VerifyOutputs` 检查 `layout_info` 顶层 `warnings`，而 Python 仅返回元素信息，实际警告缺失。

局部修复：

- `layout_compose_impl.py` 将已有几何规则集中为 `layout_geometry_warnings(layout)`；创建版面与查询版面共用，不缓存旧警告。
- 页面越界沿用 0.001 毫米容差；重叠沿用 title/overview/legend/scalebar/north_arrow 五个前景角色及 0.01 毫米阈值。主图背景、网格、装饰框与前景元素的正常叠放不参与重叠判定；装饰边框越界仍使用其真实外框。
- `print_layout_impl.py:get_layout_info_handler` 每次读取当前元素，返回顶层 `warnings`，与现有 Go 核验字段一致。

唯一执行的定向验证为既有 `test_layout_compose_qgis.py` 的 warnings 场景：正常四角版面无误报；正常统计版面无误报；产生标题越界与图例/指北针重叠，查询与创建返回同类警告；将两个元素移回合法位置，再查询没有旧警告。

结果：本地 QGIS `1 passed, 4 deselected in 2.44s`。使用隔离临时工程，GOPROXY/GOSUMDB 关闭；未调用模型、网络或收费 API，未重新运行完整门禁，未修改用户版面。
