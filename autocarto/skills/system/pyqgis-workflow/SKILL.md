---
name: pyqgis-workflow
description: PyQGIS 多步骤工作流脚本模板，支持数据处理流水线、中间结果管理和工作流错误恢复
kind: workflow
---

# PyQGIS 多步骤工作流

## 概述

本 skill 提供 PyQGIS 脚本模板，用于编排多步骤数据处理流水线。涵盖中间结果管理、步骤间数据传递、条件分支处理和错误恢复机制。

## 核心 API 参考

| 类/方法 | 用途 |
|---------|------|
| `processing.run(algorithm, params)` | 执行 Processing 算法 |
| `QgsVectorLayer("memory:", name, "memory")` | 创建内存图层 |
| `QgsProject.instance().addMapLayer(layer, False)` | 添加图层但不显示在图层树 |
| `QgsProject.instance().removeMapLayer(layer_id)` | 移除图层 |
| `QgsProcessingFeedback` | 处理进度反馈 |

## 代码模板

### 1. 数据加载→清洗→分析→制图完整流水线

```python
import processing
from qgis.core import QgsProject, QgsVectorLayer, QgsRasterLayer

project = QgsProject.instance()

# ========== 步骤 1: 数据加载 ==========
print("=== 步骤 1: 数据加载 ===")
vector_path = "/path/to/parcels.shp"
dem_path = "/path/to/dem.tif"

parcels = QgsVectorLayer(vector_path, "parcels", "ogr")
dem = QgsRasterLayer(dem_path, "dem")

if not parcels.isValid() or not dem.isValid():
    raise Exception("数据加载失败")

project.addMapLayer(parcels)
project.addMapLayer(dem)
print(f"  加载完成: 矢量 {parcels.featureCount()} 要素, 栅格 {dem.width()}x{dem.height()}")

# ========== 步骤 2: 数据清洗 ==========
print("=== 步骤 2: 数据清洗 ===")

# 修复无效几何
fixed = processing.run("native:fixgeometries", {
    'INPUT': parcels,
    'OUTPUT': 'memory:parcels_fixed'
})['OUTPUT']

# 统一 CRS
if fixed.crs().authid() != dem.crs().authid():
    fixed = processing.run("native:reprojectlayer", {
        'INPUT': fixed,
        'TARGET_CRS': dem.crs().authid(),
        'OUTPUT': 'memory:parcels_reprojected'
    })['OUTPUT']

print(f"  清洗完成: CRS={fixed.crs().authid()}, 要素数={fixed.featureCount()}")

# ========== 步骤 3: 空间分析 ==========
print("=== 步骤 3: 空间分析 ===")

# 计算坡度
slope = processing.run("native:slope", {
    'INPUT': dem,
    'Z_FACTOR': 1,
    'OUTPUT': 'TEMPORARY_OUTPUT'
})
slope_layer = QgsRasterLayer(slope['OUTPUT'], "slope")

# 分区统计：每个地块的平均坡度
stats = processing.run("native:zonalstatisticsfb", {
    'INPUT': fixed,
    'INPUT_RASTER': slope_layer,
    'RASTER_BAND': 1,
    'COLUMN_PREFIX': 'slope_',
    'STATISTICS': [2, 5, 6],  # mean, min, max
    'OUTPUT': 'memory:parcels_with_slope'
})['OUTPUT']

print(f"  分析完成: 已添加坡度统计字段")

# ========== 步骤 4: 分类与筛选 ==========
print("=== 步骤 4: 分类与筛选 ===")

# 按坡度分类
stats.startEditing()
from qgis.core import QgsField
from qgis.PyQt.QtCore import QVariant
stats.dataProvider().addAttributes([QgsField("suitability", QVariant.String, len=20)])
stats.updateFields()

suit_idx = stats.fields().indexOf("suitability")
for feat in stats.getFeatures():
    mean_slope = feat["slope_mean"]
    if mean_slope is not None:
        if mean_slope < 5:
            suitability = "优"
        elif mean_slope < 15:
            suitability = "良"
        elif mean_slope < 25:
            suitability = "中"
        else:
            suitability = "差"
    else:
        suitability = "未知"
    stats.changeAttributeValue(feat.id(), suit_idx, suitability)

stats.commitChanges()

# 提取适宜地块
suitable = processing.run("native:extractbyexpression", {
    'INPUT': stats,
    'EXPRESSION': '"suitability" IN (\'优\', \'良\')',
    'OUTPUT': 'memory:suitable_parcels'
})['OUTPUT']

project.addMapLayer(suitable)
print(f"  筛选完成: 适宜地块 {suitable.featureCount()} 个")

# ========== 步骤 5: 结果输出 ==========
print("=== 步骤 5: 结果输出 ===")
print(f"  总地块数: {stats.featureCount()}")
print(f"  适宜地块: {suitable.featureCount()}")
print(f"  适宜比例: {suitable.featureCount()/stats.featureCount()*100:.1f}%")
```

### 2. 带错误恢复的工作流

```python
import processing
from qgis.core import QgsProject, QgsVectorLayer

class WorkflowStep:
    def __init__(self, name, func):
        self.name = name
        self.func = func
        self.result = None
        self.status = "pending"
        self.error = None

    def execute(self, context):
        try:
            self.status = "running"
            self.result = self.func(context)
            self.status = "success"
            return True
        except Exception as e:
            self.status = "failed"
            self.error = str(e)
            return False

def run_workflow(steps, context):
    """执行工作流，支持错误恢复"""
    for i, step in enumerate(steps):
        print(f"[{i+1}/{len(steps)}] 执行: {step.name}")

        if not step.execute(context):
            print(f"  失败: {step.error}")

            # 尝试恢复
            if i > 0 and steps[i-1].result:
                print(f"  尝试使用上一步结果重试...")
                if step.execute(context):
                    print(f"  重试成功")
                    continue

            print(f"  工作流在步骤 {i+1} 中断")
            break
        else:
            print(f"  完成")

    # 输出报告
    print("\n=== 工作流执行报告 ===")
    for step in steps:
        status_icon = {"success": "OK", "failed": "FAIL", "pending": "SKIP"}.get(step.status, "?")
        print(f"  [{status_icon}] {step.name}")
        if step.error:
            print(f"       错误: {step.error}")

# 使用示例
context = {"project": QgsProject.instance()}

steps = [
    WorkflowStep("加载数据", lambda ctx: load_data(ctx)),
    WorkflowStep("数据清洗", lambda ctx: clean_data(ctx)),
    WorkflowStep("空间分析", lambda ctx: analyze(ctx)),
    WorkflowStep("结果导出", lambda ctx: export(ctx)),
]

run_workflow(steps, context)
```

### 3. 条件分支处理

```python
import processing
from qgis.core import QgsProject, QgsWkbTypes

project = QgsProject.instance()
layer = project.mapLayersByName("input_layer")[0]

# 根据几何类型选择不同处理路径
geom_type = layer.geometryType()

if geom_type == QgsWkbTypes.PointGeometry:
    print("检测到点图层，执行点分析流程")

    # 密度分析
    result = processing.run("native:heatmapkerneldensityestimation", {
        'INPUT': layer,
        'RADIUS': 1000,
        'PIXEL_SIZE': 100,
        'OUTPUT': 'TEMPORARY_OUTPUT'
    })
    from qgis.core import QgsRasterLayer
    heatmap = QgsRasterLayer(result['OUTPUT'], "density_heatmap")
    project.addMapLayer(heatmap)

elif geom_type == QgsWkbTypes.LineGeometry:
    print("检测到线图层，执行网络分析流程")

    # 线简化
    result = processing.run("native:simplifygeometries", {
        'INPUT': layer,
        'METHOD': 0,
        'TOLERANCE': 10,
        'OUTPUT': 'memory:simplified_lines'
    })
    project.addMapLayer(result['OUTPUT'])

elif geom_type == QgsWkbTypes.PolygonGeometry:
    print("检测到面图层，执行面积分析流程")

    # 添加面积字段
    result = processing.run("native:addgeometryattributes", {
        'INPUT': layer,
        'CALC_METHOD': 0,
        'OUTPUT': 'memory:with_area'
    })
    project.addMapLayer(result['OUTPUT'])

else:
    print(f"不支持的几何类型: {geom_type}")
```

### 4. 中间结果管理

```python
from qgis.core import QgsProject

class IntermediateManager:
    """管理工作流中间结果，支持自动清理"""

    def __init__(self):
        self.layers = {}  # name -> layer_id
        self.project = QgsProject.instance()

    def add(self, name, layer, visible=False):
        """添加中间结果图层"""
        self.project.addMapLayer(layer, visible)
        self.layers[name] = layer.id()
        return layer

    def get(self, name):
        """获取中间结果图层"""
        layer_id = self.layers.get(name)
        if layer_id:
            return self.project.mapLayer(layer_id)
        return None

    def cleanup(self, keep=None):
        """清理中间结果，可选保留指定图层"""
        keep = keep or []
        for name, layer_id in list(self.layers.items()):
            if name not in keep:
                self.project.removeMapLayer(layer_id)
                del self.layers[name]
        print(f"已清理中间结果，保留: {keep}")

    def report(self):
        """输出中间结果报告"""
        print("=== 中间结果 ===")
        for name, layer_id in self.layers.items():
            layer = self.project.mapLayer(layer_id)
            if layer:
                print(f"  {name}: {layer.featureCount()} 要素")

# 使用示例
import processing

mgr = IntermediateManager()

# 步骤1
layer = QgsProject.instance().mapLayersByName("input")[0]
fixed = processing.run("native:fixgeometries", {
    'INPUT': layer, 'OUTPUT': 'memory:fixed'
})['OUTPUT']
mgr.add("fixed", fixed)

# 步骤2
buffered = processing.run("native:buffer", {
    'INPUT': mgr.get("fixed"), 'DISTANCE': 100, 'OUTPUT': 'memory:buffered'
})['OUTPUT']
mgr.add("buffered", buffered)

# 步骤3
dissolved = processing.run("native:dissolve", {
    'INPUT': mgr.get("buffered"), 'OUTPUT': 'memory:dissolved'
})['OUTPUT']
mgr.add("result", dissolved, visible=True)

mgr.report()
mgr.cleanup(keep=["result"])  # 只保留最终结果
```

## 脚本编写规范

1. 每个步骤开始时打印步骤名称和序号，便于追踪进度。
2. 步骤间通过变量传递中间结果（图层对象或文件路径）。
3. 中间图层使用 `'memory:'` 前缀，最终结果保存到文件。
4. 每个步骤包含 try-except 错误处理，失败时记录错误并决定是否继续。
5. 工作流完成后输出汇总报告，包含每步状态和关键统计。
6. 临时中间图层在不再需要时及时清理，释放内存。

## 常见陷阱

- 内存图层在 QGIS 关闭后丢失，重要结果必须保存到文件
- `processing.run()` 返回的 `OUTPUT` 可能是路径字符串（需要重新加载）或图层对象
- 步骤间传递图层时，确保图层仍然有效（未被删除或关闭）
- 多步骤工作流中 CRS 不一致是最常见的错误源，建议在第一步统一
- `addMapLayer(layer, False)` 的第二个参数为 False 时图层不显示在图层树，适合中间结果
- 长时间运行的工作流应定期调用 `iface.mapCanvas().refreshAllLayers()` 刷新显示
