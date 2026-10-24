# PyQGIS 几何操作

本参考整理自原方法包pyqgis-geometry-ops。仅在现成工具不能表达操作时使用；输入图层ID、字段、参数和输出位置从已核验上下文取得，示例不能作为固定默认。

## API与操作

QgsGeometry.fromPointXY/fromPolylineXY/fromPolygonXY、buffer、simplify、centroid、pointOnSurface、makeValid、intersection、combine、unaryUnion、QgsSpatialIndex。

## 参数与执行

输入几何先查空值和有效性，面环闭合；输出几何类型与字段匹配。centroid可能在面外，需要内部点时用pointOnSurface。大量合并优先有界分批或unaryUnion，保留来源标识并检查修复后部件和类型变化。

## 检查与边界

空间索引只生成候选，intersects和最近邻后仍须精确几何判断。缓冲、简化和距离阈值确认单位，不能直接把度当米；provider创建新内存要素与编辑已有数据的事务规则不同。

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
