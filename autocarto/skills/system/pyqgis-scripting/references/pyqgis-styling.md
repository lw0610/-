# PyQGIS 符号与标注

本参考整理自原方法包pyqgis-styling。仅在现成工具不能表达操作时使用；输入图层ID、字段、参数和输出位置从已核验上下文取得，示例不能作为固定默认。

## API与操作

QgsGraduatedSymbolRenderer、QgsCategorizedSymbolRenderer、QgsRuleBasedRenderer、QgsSingleSymbolRenderer、QgsSymbol、QgsStyle、QgsPalLayerSettings、QgsTextFormat。

## 参数与执行

连续数值、离散类别和规则表达使用不同渲染器；按分布与任务选分类方法、断点和色带，不固定Jenks或某种配色。数据类型和值类型一致，空值有独立表达。统一符号会替换既有分级，设置标注不应覆盖渲染器。

## 检查与边界

设置classAttribute与classificationMethod后更新类别，核实真实ranges、颜色及类别数量；标注字段取真实schema，字体查实际可用字库，遵从版式锁定样式。triggerRepaint只刷新，不等于保存工程，报告真实renderer状态。

显示和叠加使用 QGIS 动态投影；只有分析、量算明确需要时才转换。先核实源 CRS、坐标单位和算法处理方式，不能因显示 CRS 不同而批量重投影；定义 CRS 只改元数据，重投影才改变坐标。转换时生成独立结果，核对范围、位置和精度。

## 参数化调用骨架

```python
from qgis.core import QgsProject

def require_layer(layer_id):
    layer = QgsProject.instance().mapLayer(layer_id)
    if layer is None or not layer.isValid():
        raise ValueError("输入图层ID无效")
    return layer

# layer_id和checked_parameters必须来自已核验的工具返回与当前任务。
# 执行前检查提供者实际支持的参数，执行后核验真实结果。
```
