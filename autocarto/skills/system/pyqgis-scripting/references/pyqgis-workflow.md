# PyQGIS 多步骤脚本

本参考整理自原方法包pyqgis-workflow。仅在现成工具不能表达操作时使用；输入图层ID、字段、参数和输出位置从已核验上下文取得，示例不能作为固定默认。

## API与操作

QgsProject、processing.run、QgsProcessingFeedback；显式的步骤状态、上下文和本次临时资源清单。

## 参数与执行

步骤间传递真实图层对象或返回路径，检查OUTPUT可能是对象或文件。前置失败就停止依赖步骤，恢复须修正原因并确认操作可重复，不能原样无条件重试。中间产物命名与来源可追溯，重要结果独立持久化。

## 检查与边界

按任务选择执行分支，不内置某种专题流水线。临时资源只能按本次记录的ID清理，禁止扫描并删除其他图层。完成报告每步状态、参数、输出、错误和未完成项；QGIS关闭会丢失内存层。

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
