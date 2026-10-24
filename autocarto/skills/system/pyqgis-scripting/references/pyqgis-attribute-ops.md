# PyQGIS 属性字段

本参考整理自原方法包pyqgis-attribute-ops。仅在现成工具不能表达操作时使用；输入图层ID、字段、参数和输出位置从已核验上下文取得，示例不能作为固定默认。

## API与操作

QgsField、QgsExpression、QgsExpressionContext、QgsExpressionContextUtils；startEditing/commitChanges/rollBack、changeAttributeValue、updateFields。

## 参数与执行

添加字段前查重，fields().indexOf返回-1应中止；按真实字段类型构建表达式并检查解析/求值错误。去重按已确认的主键，连接前核实两侧类型、重复键和一对多关系。编辑失败回滚，不混用provider直接写入和编辑缓冲修改同一数据；commitChanges失败记录commitErrors。

## 检查与边界

native:joinattributestable、native:removeduplicatesbyattribute、native:refactorfields。面积与比率计算需要有依据的量算单位和非零分母，不能把任何数值字段当指定指标。

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
