# PyQGIS 批量处理

本参考整理自原方法包pyqgis-batch-processing。仅在现成工具不能表达操作时使用；输入图层ID、字段、参数和输出位置从已核验上下文取得，示例不能作为固定默认。

## API与操作

Path.iterdir/rglob、processing.run、QgsVectorFileWriter、QgsProcessingFeedback。

## 参数与执行

从任务数据清单或明确目录枚举有限输入，按扩展名识别并验证有效性；不要递归扫描整台机器。每项独立记录输入、输出、成功/失败和错误，失败不伪装成功，依赖失败时停止后续依赖。清理仅限本次创建的临时对象，输出命名避免覆盖。

## 检查与边界

native:savefeatures、native:clip、gdal:cliprasterbymask。批量导出检查实际返回码及文件；只重试可重复且修正了原因的失败项，不无条件重复修改原数据。

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
