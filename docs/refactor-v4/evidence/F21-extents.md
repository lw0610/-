# F21 补充：获取范围与裁剪面

## 修正

- `resolveExtent` 仅把纯 `Polygon` / `MultiPolygon` GeoJSON（可在 Feature / FeatureCollection 中）作为裁剪面。点、线、混合要素和 GeometryCollection 使用外包框，不将其路径误当作面掩膜。
- 按 `02` §3.4 规定，外扩操作以原数据外包框为基础。只要外扩比例大于 0，就清除原始裁剪面路径，确保规范化使用扩大后的 bbox，而不会再次裁回未扩大的原面。
- 用户输入和上游步骤产出使用同一个校验与外扩函数。WGS84 bbox 必须有限、顺序正确、有面积；NaN / Inf 不进入响应、缓存或获取请求。`places.csv` 含此类数值时整项报错；缺失坐标行仍按原逻辑跳过。
- 外部 QGIS 元数据 resolver 目前不提供几何类型，无法证明其 Path 是有效面。因此使用其经纬度 bbox，清除未经确认的 Path。已知投影 CRS 的 GeoJSON 仍可通过 resolver 获取 WGS84 bbox。

## 零面积范围的合理限制

规范没有给出点或水平/竖直路线的最小缓冲距离。原来的 0.01° 固定兜底缺乏依据，现已删除。此类**获取范围**无法仅凭百分比变成有面积的区域，会明确提示“另行指定有面积的区域或边界”；不会猜测行政区或额外扩大固定距离。

这与获取结果的几何不同：查询某个有面积的区域后，结果只有一个驻地点或一条水平铁路仍可有效，已由 F51 的 `ValidateStats` 和真实 QGIS 验收覆盖。

## 验证

`planner_extent_test.go` 覆盖纯面/多面/集合、点线与混合几何、外扩后的 bbox/Path、输入和步骤产出、CSV NaN/Inf、GeoJSON 非法坐标、外部 resolver 非有限/逆序范围、零面积 GeoJSON 与路线 CSV、投影 GeoJSON 元数据回退；检查错误不会留下不能 JSON 序列化的计划。

在隔离 `AUTOCARTO_HOME`、禁用 keyring 的环境运行：

`go test ./internal/acquire ./internal/flow ./internal/flowrun -count=1`

全部 PASS，分别 1.33 / 5.58 / 5.24 秒。没有执行真实在线获取、没有读取用户运行数据。
