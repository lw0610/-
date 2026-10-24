# PyQGIS 地形分析

本参考整理自原方法包pyqgis-terrain-ops。仅在现成工具不能表达操作时使用；输入图层ID、字段、参数和输出位置从已核验上下文取得，示例不能作为固定默认。

## API与操作

processing.run与QgsRasterLayer；native:slope/aspect/hillshade/ruggednessindex，gdal:slope/aspect/hillshade/contour，已注册的GRASS水文算法。

## 参数与执行

先核实DEM的水平/垂直单位、分辨率、NoData和范围；Z_FACTOR、SCALE含义依算法不同，不用固定111120处理所有地理坐标。等高距按数据尺度选择，阴影方位角是光源方向，记录输出坡度单位和坡向约定。

## 检查与边界

水文threshold与流向规则、视域观察高度/目标高度/距离以实际参数定义为准。GRASS提供者未注册就说明缺失，不假定grass7编号永久可用。检查边缘、空洞、值域和输出范围，保留算法与参数。

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
