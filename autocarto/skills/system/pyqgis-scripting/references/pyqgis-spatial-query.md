# PyQGIS 空间查询

本参考整理自原方法包pyqgis-spatial-query。仅在现成工具不能表达操作时使用；输入图层ID、字段、参数和输出位置从已核验上下文取得，示例不能作为固定默认。

## API与操作

QgsExpression、QgsFeatureRequest、setFilterExpression、setFilterRect、setSubsetOfAttributes、setLimit、QgsSpatialIndex。

## 参数与执行

字段用双引号、字符串用单引号，NULL用IS NULL，表达式须检查解析错误。限定字段与返回量以控制内存，迭代器只能消费一次。矩形/索引过滤只给候选，复杂空间关系再精确判断，输入坐标统一在计算所用空间中。

## 检查与边界

选择集的Set/Add/Remove模式要明确，selection不生成新图层；按需extractbyattribute/expression/location产生独立结果。谓词枚举取自当前算法定义，不能从旧模板猜编号。保留原选择或按任务清理，空结果如实报告。

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
