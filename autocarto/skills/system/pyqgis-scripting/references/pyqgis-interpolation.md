# PyQGIS 空间插值

本参考整理自原方法包pyqgis-interpolation。仅在现成工具不能表达操作时使用；输入图层ID、字段、参数和输出位置从已核验上下文取得，示例不能作为固定默认。

## API与操作

QgsIDWInterpolator、QgsTinInterpolator、QgsInterpolator.LayerData、QgsGridFileWriter、QgsRasterBandStats。

## 参数与执行

插值输入数据结构以本机算法parameterDefinitions为准，核实字段索引、Z值来源、点/结构线/断裂线类型，不照抄旧序列化字符串。IDW幂次和邻域影响局部性，TIN方法按线性或平滑需求选；像元大小匹配样点密度，不能任意提升分辨率。

## 检查与边界

qgis:idwinterpolation、qgis:tininterpolation仅在实际注册时使用。GRASS v.surf.rst是正则化样条，不能称为Kriging。训练点原位误差不能称独立交叉验证；留出或空间验证并报告MAE/RMSE和样本数。

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
