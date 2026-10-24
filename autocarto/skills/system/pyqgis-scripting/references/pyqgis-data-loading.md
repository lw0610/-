# PyQGIS 数据加载与检查

本参考整理自原方法包pyqgis-data-loading。仅在现成工具不能表达操作时使用；输入图层ID、字段、参数和输出位置从已核验上下文取得，示例不能作为固定默认。

## API与操作

QgsVectorLayer、QgsRasterLayer、isValid、featureCount、fields、crs、extent、QgsProject.addMapLayer。

## 参数与执行

已分配现成加载工具时优先调用；脚本用于工具无法表达的提供者URI。输入路径从项目根或资源基准解析结果取得，不靠cwd。CSV URI按真实分隔符、坐标字段、编码和已知CRS构建，GPKG明确子图层，远程URI使用实际来源与服务参数。

## 检查与边界

有效后才加入工程，记录真实图层ID；检查数量、范围、CRS、字段/波段和缺测。空图层统计避免除零，NULL与零分开；缺少prj不能一律setCrs为WGS84。

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
