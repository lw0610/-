# PyQGIS 栅格分析

本参考整理自原方法包pyqgis-raster-analysis。仅在现成工具不能表达操作时使用；输入图层ID、字段、参数和输出位置从已核验上下文取得，示例不能作为固定默认。

## API与操作

QgsRasterLayer、bandCount、width/height、rasterUnitsPerPixelX/Y、dataProvider.block、bandStatistics、QgsRasterBandStats。

## 参数与执行

波段与物理量由源元信息确定，不能固定红/近红外编号。逐像元计算先核实分析网格、范围、原点与分辨率，区分类别和连续值重采样；NoData是掩膜语义而非普通-9999算术。表达式处理零分母和溢出，输出数据类型明确。

## 检查与边界

native:rastercalculator、gdal:cliprasterbyextent、gdal:cliprasterbymask、gdal:merge、gdal:warpreproject、native:reclassifybytable、native:zonalstatisticsfb。重分类边界与统计指标枚举以真实schema为准，不能照抄旧索引。

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
