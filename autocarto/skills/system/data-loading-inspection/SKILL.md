---
name: data-loading-inspection
description: 多源数据加载与质量检查，支持矢量/栅格数据的格式识别、加载验证和质量报告生成
kind: workflow
---

# 多源数据加载与质量检查

## 概述

本 skill 提供标准化的数据加载与质量检查流程，确保数据在进入分析流程前经过完整验证。支持 SHP、GeoJSON、GPKG、GeoTIFF、DXF、CSV 等常见 GIS 格式的自动识别和加载。

## 前置条件

- 数据文件路径已知且文件存在；若路径未知，先检查 `get_project_info` 返回的工程目录 `data_files`
- QGIS 工程已打开（通过 `get_project_info` 确认）
- 如需加载远程数据，先用 `file_downloader` 下载到本地

## 工作流步骤

### 1. 格式识别与加载

- 如果工程目录 `data_files` 中已经存在与任务主题/地区明显匹配的文件，优先直接使用这些文件，不要先向用户追问路径
- 根据文件扩展名判断数据类型：
  - 矢量格式（.shp/.geojson/.gpkg/.dxf/.csv/.kml）→ `data_load_vector`
  - 栅格格式（.tif/.tiff/.img/.asc/.nc）→ `data_load_raster`
  - CSV 含坐标列 → `data_load_vector`，需指定 X/Y 字段和 CRS
  - 不确定格式 → 先尝试矢量加载，失败再尝试栅格
- **判断标准**：`get_layers_info` 返回中包含新加载的图层名

### 2. 加载验证

- 使用 `get_layers_info` 确认图层已正确加载到工程中
- 使用 `get_active_layer_info` 检查图层详细信息：CRS、字段结构、要素范围；如果没有活动图层，先按图层名查询，不要把“无活动图层”等价成“无可用数据”
- **判断标准**：图层要素数 > 0，CRS 非空，空间范围合理

### 3. 质量检查

- 矢量数据：
  - 使用 `check_validity` 检测无效几何，记录无效数量和比例
  - 检查空值字段：通过 `get_active_layer_info` 的字段统计识别
- 栅格数据：
  - 使用 `raster_info` 检查元数据（波段数、数据类型、NoData 值）
  - 使用 `raster_statistics` 检查值域范围，识别异常值
- **判断标准**：无效几何比例 < 5%，无全空字段，栅格值域在合理范围内

### 4. 质量报告输出

- 汇总报告内容：
  - 要素/像元总数
  - 无效几何数量和比例（矢量）
  - 空值字段统计
  - CRS 信息（EPSG 代码和投影名称）
  - 数据范围（Extent）
  - 数据完整性评估（优/良/差）

## 常见问题处理

- 加载失败提示编码错误 → 尝试指定 encoding 参数（UTF-8/GBK/GB2312）
- SHP 缺少 .prj 文件 → 加载后用 `crs_define_projection` 手动定义 CRS
- CSV 坐标列识别失败 → 明确指定 X/Y 字段名和分隔符
- 栅格加载后显示全黑 → 检查 NoData 值设置，用 `raster_statistics` 确认值域
- 文件路径含中文或空格 → 确认路径编码正确

## 输出检查清单

- [ ] 图层已成功加载到工程中
- [ ] CRS 信息正确且非空
- [ ] 要素/像元数量符合预期
- [ ] 无效几何已识别并记录
- [ ] 质量报告已输出
