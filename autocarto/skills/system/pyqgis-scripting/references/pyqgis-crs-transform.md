# PyQGIS 坐标系诊断与转换

本参考整理自原方法包pyqgis-crs-transform。仅在现成工具不能表达操作时使用；输入图层ID、字段、参数和输出位置从已核验上下文取得，示例不能作为固定默认。

## API与操作

QgsCoordinateReferenceSystem、QgsCoordinateTransform、QgsProject.transformContext、transform/transformBoundingBox。

## 参数与执行

显示和叠加使用 QGIS 动态投影；只有分析、量算明确需要时才转换。先核实源 CRS、坐标单位和算法处理方式，不能因显示 CRS 不同而批量重投影；定义 CRS 只改元数据，重投影才改变坐标。转换时生成独立结果，核对范围、位置和精度。先查crs.isValid、authid和mapUnits，未知源坐标需可靠依据。layer.setCrs不转换坐标，不能用它修正显示偏移。

## 检查与边界

native:reprojectlayer与gdal:warpreproject只对选定输入执行。目标CRS按研究范围与误差需求传入，不固定投影带；坐标点和矩形均携带源CRS，转换后核验范围与位置。

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
