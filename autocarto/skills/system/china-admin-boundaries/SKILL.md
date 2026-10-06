---
name: china-admin-boundaries
description: 使用 GeoCode 自带完整中国省、市、县行政区多边形准备本地研究区；按行政区代码或唯一名称选取，供 GEE 和 QGIS 使用，无需用户已有边界文件
kind: workflow
enabled: true
requires_tools:
  - gee_run_script
---

# China Administrative Boundaries

GeoCode's bundled **official, compliant** polygons of China's administrative divisions,
sourced from **Tianditu** (China's national geospatial platform) and following the national
mapping standard. Whenever a task needs China's province / city / county areas, use these —
**do not** download them from OSM / Natural Earth / GADM, whose border depiction is not
compliant for use in mainland China.

## Data

Three GeoJSON files in this skill's `data/` directory (use the absolute path provided to
you), CRS **WGS84 / EPSG:4326**:

| File | Level | Features |
| --- | --- | --- |
| `china_province.geojson` | province | 34 |
| `china_city.geojson` | city (prefecture) | 375 |
| `china_county.geojson` | county / district | 2891 |

Fields: `adcode` (6-digit code, **unique key**), `name` (Chinese name), `gb` (`156` + adcode), `level`.

## Selecting a unit

Select by **`adcode`, not `name`** — county-level names repeat. `adcode` is
`province(2) + city(2) + county(2)`, so prefix-match to drill down: `51` → all of Sichuan,
`5101` → all of Chengdu.

## Mapping note

These are analysis polygons only — they **exclude** the nine-dash line. To draw a
whole-China map, use the `thematic-map` skill's `china_base` as the national frame (it carries
the nine-dash line) and overlay these polygons as a thematic layer.

## AutoCarto 本地调用适配

以上为上游技能完整正文；数据和许可原样保留，具体固定提交、原文与数据哈希见 `UPSTREAM.md`、`upstream-manifest.json`。上游对数据来源和用途的说明是来源声明；不能据此声称已独立核实数据时效或获得任何制图审批。

中国行政区研究任务需要本地边界时，优先使用本包原数据。QGIS 没有图层并不表示边界不存在，也不需要因此让用户提供文件路径：先查询本包，再导出目标区域到当前会话的 `output_dir`。无需重新联网下载边界、登录地图网站或打开浏览器。不要用粗略经纬度矩形代替行政区，也不要把 GCJ-02 数据直接声明成 WGS84。用户明确给定自定义区域时尊重该区域，不以行政区数据覆盖用户条件。

skill 返回绝对 base directory 后，用 `gee_run_script` 读取并复用 `templates/select_boundary.py`。该模板不联网，不初始化 EE，按源文件完整字节哈希核验，再检查字段、声明 CRS、唯一代码及选中多边形的有效性；不自动修复或简化原几何。

```python
from pathlib import Path
import json
import runpy

# skill_base_dir 来自本技能返回的绝对 base directory。
# administrative_name / administrative_level 由用户任务确定，不能照抄固定城市。
selector = runpy.run_path(str(Path(skill_base_dir) / "templates/select_boundary.py"))
candidates = selector["find_candidates"](
    name=administrative_name, level=administrative_level,
)
print(json.dumps(candidates, ensure_ascii=False), flush=True)
```

`level` 是 `province`、`city` 或 `county`；不知道层级时省略，查询三个原始图层。名称查询默认精确匹配；简写或未匹配时可用 `contains=True` 只查看候选，核对完整名称、层级和代码后选择。不得把候选中心点或第一条重名结果当成边界。只有候选确有歧义而任务上下文无法区分、该地区不在数据中或源数据/几何验证失败时才需要进一步澄清或选择其他有据可查的边界来源。

候选唯一且符合用户任务时，按其实际代码导出（也支持唯一完整名称直接导出）：

```python
if len(candidates) != 1:
    # 返回候选供结合任务上下文消歧；不要下载影像或猜测第一项。
    raise ValueError("需要核对行政区候选，尚未生成研究区")
selected = candidates[0]
result = selector["export_boundary"](
    output_dir,
    adcode=selected["adcode"],
    level=selected["level"],
    expected_name=selected["name"],
)
print(json.dumps(result, ensure_ascii=False), flush=True)
roi = load_region(result["boundary_path"]).geometry()
```

返回 `boundary_path`、`provenance_path`、`selected`、`geometry` 和 `artifact_paths`。GeoJSON 始终恰好一个 Polygon/MultiPolygon，要素属性和原坐标保持不变；CRS84 是经度在前、纬度在后的 WGS84，供 `load_region` 读取为 EPSG:4326。provenance 记录源提交、字节哈希、筛选身份和几何核验。结果保留在当前 `output_dir`；后续直接引用该路径，不能再复制到工程外目录。

拿到已验证的本地边界后，接续 `gee-scripting` 的候选筛选、掩膜、覆盖检查与影像下载。只需要一个行政区时不合并其他单元；跨多个地区的明确任务先选齐相应要素，再由具备 GIS 工具的专家进行所需合并和验证。原包的全国框线用途说明保留，不把本包分析边界误当全国地图完整要素。
