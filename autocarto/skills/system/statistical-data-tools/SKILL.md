---
name: statistical-data-tools
description: 行政区年度统计数据核验与独立派生工具，仅供数据工程角色使用
kind: category
runtime: qgis
enabled: true
tools:
- data_prepare_statistical
display_name: 统计数据准备
domain: 数据
system: false
hidden: false
auto_mount: ''
---

# 统计数据准备使用说明

## 1. 核对输入与口径

若工程中已有包含所需数值字段的行政区面，先用 `get_layers_info` / `get_active_layer_info` 核验真实图层、几何、字段类型、年份、指标、单位、来源及缺测状态，直接交接已核验的真实字段，不强制重新寻找 CSV 或生成 `stat_*` 字段。不能仅因有数值字段就推断统计口径；无法确认的项目需明确列出。以下关联步骤仅用于新 CSV 数据。

使用工程信息和现有数据确认行政区面、统计 CSV 与年份。传入真实 `geojson_path`、`csv_path`、`year`；GeoJSON 必须是行政区面数据。若只有其他格式的行政区，先通过已有数据加载/导出工具生成独立 GeoJSON，不覆盖原数据，不因显示 CRS 不同而强行重投影。

- `name_field` 默认 `name`：行政区名称属性。
- `table_format` 默认 `wide_year_rows`：一行一个年份，地区作为列；`year_field` 默认 `年份`。
- 长表传 `table_format=long`，并按真实字段设置 `region_field`（默认 `地区`）和 `csv_value_field`（默认 `数值`）。每次只准备所选 `year` 的单一指标；同一年包含多个指标的长表，应先按用户指定指标筛选为独立 CSV。`indicator` 仅校验元数据，不是筛选参数，禁止把多个指标混在一起关联或分级。
- `value_field` 默认 `stat_value`，是派生面上用于分级设色的数值字段。
- `metadata_fields` 默认识别 `indicator→指标`、`unit→单位`、`source→数据来源`、`database→数据库`；这些列不得当成行政区。确有其他表头时按真实字段配置。
- `indicator`、`unit`、`source`、`database` 仅填已知事实。显式值与表中元数据冲突会报错，应核实而非伪造覆盖；缺失元数据的警告必须随结果交接。
- `exclude_names` 只列任务明确排除的地区，不能用排除掩盖名称匹配错误。`output_dir` 可选，工具始终创建独立运行子目录；默认优先写入当前会话的 `intermediate/statistical`，无会话时回退到 `AUTOCARTO_HOME/data/statistical`。

## 2. 先验证，再生成

先以 `validate_only=true` 调用 `data_prepare_statistical`，检查返回 `data.metadata`、`counts`、`unmatched_csv_regions`、`unmatched_geojson_regions`、`missing_regions`、`excluded_regions` 和 `value_range`。

`allow_unmatched` 默认为 false：非排除地区在 CSV 或行政区任一侧缺失时，工具失败但保留报告。先检查表格式、年份、关联字段、元数据列和真实地区名称。若问题仅为部分未匹配，且仍有足够有效数值可分级，保留并交接完整未匹配清单，以 `allow_unmatched=true` 继续生成；无需逐项人工确认。未匹配保持 NULL，以灰底和文字说明，不猜测别名、不填零，也不删除问题记录。重复键、非法数值或没有可用数据等错误仍须定位原因，不能用该参数绕过或声称成功。空单元格是缺测，零是有效值，二者必须区分。

检查报告后，用相同输入及已核验参数调用 `validate_only=false` 生成；仅部分未匹配按上述规则调整 `allow_unmatched`。保留真实返回的 `data.paths` 路径，不自行拼接输出位置；外层 `output` 是 manifest，不能把它当矢量图层加载：

- `paths.polygons_geojson`：含数值和状态的派生行政区面。
- `paths.boundaries_geojson`：边界线数据，按制图需要使用。
- `paths.long_csv`：规范化长表。
- `paths.manifest`：数据来源、口径和关联质量清单。

## 3. 加载与交接

调用 `data_load_vector(path=paths.polygons_geojson, layer_name=<本次唯一名称>)`，再用 `get_layers_info` / `get_active_layer_info` 核对返回的真实图层、几何、字段及 CRS。不要直接在原行政区上写统计字段。

派生属性包括指定的 `value_field`，以及 `stat_region`、`stat_year`、`stat_indicator`、`stat_unit`、`stat_source`、`stat_database`、`data_status`。状态有 valid/missing/unmatched/excluded；非有效状态保持 NULL，不转换为零，不参与数值分类。

输出：实际专题图层及路径、`value_field`、标注字段 `stat_region`、metadata、counts、各类问题名单、value_range、四项产物路径、原行政区路径和原始任务。没有匹配数值、只有一个不同有限值或工具报告错误时，不把它当作已经就绪的分级专题数据。

报告存在 missing/unmatched/excluded 等 NULL 状态且任务需要保留这些地区时，再次调用 `data_load_vector` 加载同一个 `paths.polygons_geojson`，传 `reuse_existing=false` 并给灰底独立且唯一的图层名称。这会创建两个独立图层对象，不改写派生文件；由拥有数据加载工具的调用者补载。交接专题与灰底两个真实图层名及 ID，仅对灰底设置低饱和灰色并将其放在专题层下。图例只列专题层，并另写缺测说明；排除地区不当作有效统计值。
