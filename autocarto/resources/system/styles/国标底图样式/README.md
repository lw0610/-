# 国标底图样式

本样式集由项目原点、线、面预设通过实际 QGIS 导出，保留原符号、单位、线宽、虚线、标注字号、字体、缓冲和河流合并设置。名称沿用已有预设；此名称不代替对外部制图标准的独立认证。

`resource.yaml` 声明稳定编号 `style_set.gb-basemap`，`对照表.csv` 维护图层名、别名与标注字段，QML 保存视觉效果。主名称先精确匹配，再匹配顿号或分号分隔的别名；标注字段空白时保留 QML 设置，填“无”时关闭标注。该公共表只保留通用别名。

流程可用公共编号或显示名引用，后端 Go Resolver 解析成句柄再交给样式工具。用户修改时复制资源；需要替换所有该内置资源引用时，在用户副本清单明确 `overrides: style_set.gb-basemap`。仅同名不会自动覆盖。

隐藏旧预设工具的兼容索引位于 `autocarto/tools/system/cartography/assets/basemap-legacy-presets.json`，冻结 QML 位于同域 `assets/basemap-legacy-styles/`。这些是已登记的旧入口兼容资产，正式样式集工具不读取它们；修改当前公共样式不会改变冻结兼容资产。

开发者可用 QGIS Python 运行仓库根的 `scripts/resources/export_basemap_styles.py` 检查历史 QML 导出过程。该脚本保留旧构造，仅用于生成与回归，运行时不导入；重新生成后仍须按当前公共资源规则核对对照表、清单与摘要，不能直接恢复历史场景别名。导出只保存符号与标注，避免附带随机高程样式、字段表单或数据源配置。
