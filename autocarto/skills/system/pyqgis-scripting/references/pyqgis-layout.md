# PyQGIS 版面编排

本参考整理自原方法包pyqgis-layout。仅在现成工具不能表达操作时使用；输入图层ID、字段、参数和输出位置从已核验上下文取得，示例不能作为固定默认。

## API与操作

QgsPrintLayout、initializeDefaults、QgsLayoutItemMap/Label/Legend/ScaleBar/Picture、QgsLayoutPoint/Size、QgsLayoutExporter。

## 参数与执行

优先layout_compose或QPT模板工具，脚本只处理现成工具无法表达的部分。先核实真实布局名/ID与图层ID，地图范围转换到地图框CRS，setLayers固定本次图层，图例和比例尺setLinkedMap到真实地图项。

## 检查与边界

页面位置和尺寸使用明确毫米单位；遵从版式字号与锚点，不在示例写固定专题布局。导出检查QgsLayoutExporter.Success返回码、实际文件及页面完整性，临时布局只清理本次创建对象，不能按重名直接删除已有成果。

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
