# PyQGIS 矢量空间分析

本参考整理自原方法包pyqgis-vector-analysis。仅在现成工具不能表达操作时使用；输入图层ID、字段、参数和输出位置从已核验上下文取得，示例不能作为固定默认。

## API与操作

processing.run、processing.algorithmHelp、QgsProcessingFeedback、QgsVectorLayer、QgsFeatureRequest。

## 参数与执行

按分析问题选择intersection/union/difference/clip、buffer、距离矩阵、最短路径或聚类。输入顺序决定差集和属性来源；叠加几何先核实有效性，距离/缓冲确认单位与量算方法，不为显示统一投影。

## 检查与边界

参数从实际processingRegistry算法定义取得，最近邻数量、容差、端点样式和成本模型按任务选择。检查数量、几何、字段、范围与统计；空结果允许但须解释，不按固定面积删碎片。

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
