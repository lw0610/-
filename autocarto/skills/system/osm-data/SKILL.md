---
name: osm-data
display_name: 开放地图数据（OSM）
description: 查找、统计、下载开放地图数据，保留来源、字段和缺失情况。
kind: category
domain: 在线数据
runtime: internal
enabled: true
system: false
hidden: false
tools:
- osm_check_environment
- osm_search
- osm_probe
- osm_get_objects
- osm_get_features
---

# 开放地图数据

按国内行政区获取数据，先复用或提取内置的对应级别边界，使用 `file:边界GeoJSON绝对路径` 作为 osm_probe 与 osm_get_features 的同一查询范围。例如获取河南省道路用河南省的省界文件，不用省界外接矩形，也不用市域图层的外接矩形替代。市域面可以另行提取用于制图。已有边界无需再用 osm_search 搜索同一地区。没有可用边界时，才用 osm_search 查找并按地区上下文选定关系 R；不能用地名点代替行政区面。两种搜索引擎均命中的候选优先，排除无关公交线路噪声，多校区、多部件实体按实际需要全选；无法区分同名对象时才澄清。

批量类别请求先用 osm_probe 统计；如果不确定当地采用哪些标签，传 `key` 查看标签值分布，再确定标签并集。已选定的少量对象 ID 可直接交给 osm_get_objects，无需先做范围统计。工具负责网络请求、重试和几何组装，不另写脚本或命令绕过它们下载 OSM。下载后用 geodata_inspect 核验坐标系、数量、范围、几何类型与核心字段，报告实际输出路径；结果包含多个文件时逐一核验，不能只检查第一个。

单一 `file:` 范围的直接下载在 QGIS 连接可用时自动按该原始边界精确裁剪。Overpass 的范围查询只筛选完整对象，并非几何裁剪；`bbox:`、`area:`、`around:` 或多个范围的下载不能据此宣称已按行政区裁剪。只有结果明确返回 `clipped=true` 才可报告精裁完成；失败时检查已下载路径，用原边界继续本地裁剪，不为裁剪问题重复联网下载。

按名称取一组对象用 osm_get_objects；某范围内全部某类要素用 osm_get_features。对象超过约10个改为范围查询；范围最多12个，统计超过约3万要素时按下级行政区拆分。全国数据优先现成区域提取包。兴趣点用 `types: nwr`、`geometry: centroid`，不能只取 node，否则面状医院、学校等会遗漏；区域面用 `types: way,relation`、`geometry: original`。

osm_probe 与 osm_get_features 接受同样的范围：`area:R编号` 仅用于关系；闭合 way 先用 osm_get_objects 下载，再使用其文件范围。`bbox:南,西,北,东` 是明确矩形范围，`around:半径米,纬度,经度` 是邻域，`file:/绝对路径.geojson` 必须是 WGS84 面边界。需要重投影时交给已配置的相应工具处理，不修改坐标标签冒充转换，也不将边界偷偷替换为矩形。

搜索无相关候选时，补全正式名称、加入地区上下文、尝试其他语言，或改成研究区内的类别请求；不能编造对象 ID 或把不相关的同名对象当作结果。

响应等待/停滞超时默认120秒：等待响应头或持续没有新数据时超时，持续收到数据不会仅因超过120秒中止。整次获取（含切换节点）默认总时限600秒，最多尝试3个节点；两项均可在扩展服务设置中调整。无效或不可用节点会被排除，达到次数或总时限后返回实际失败原因，不无限轮询。不要靠反复发起相同调用重置预算；根据失败原因缩小范围、改正条件或由用户决定是否重试。

标签数组表示并集，可用键=值、键、键~正则、原始括号表达式：

| 类别 | 标签 |
| --- | --- |
| 道路 | highway |
| 铁路 | railway=rail |
| 地铁轻轨 | railway=subway、railway=light_rail |
| 公交轨道站点 | highway=bus_stop、railway=station、railway=halt、public_transport=platform |
| 河流 | waterway=river |
| 水域 | natural=water、waterway=riverbank |
| 医疗 | amenity=hospital、amenity=clinic、amenity=doctors、amenity=pharmacy、amenity=dentist、healthcare、building=hospital |
| 餐饮 | amenity=restaurant、amenity=fast_food、amenity=cafe、amenity=food_court |
| 学校 | amenity=school、amenity=university、amenity=college、amenity=kindergarten |
| 商业 | shop、amenity=marketplace、amenity=mall |
| 公园绿地 | leisure~park\|garden、landuse=recreation_ground、boundary=protected_area；草地另按需求加 landuse=grass |
| 建筑 | building |
| 行政边界 | boundary=administrative |

中文制图指定 fields: [name:zh]；道路按用途加 highway、ref、lanes。固定保留 osm_id、osm_type、name、category 和查询标签键。其他标签默认丢弃并按键报告；fields: ["*"] 将原始标签保存为 osm_tags JSON。下载结果按点/线/面分文件，原始WGS84坐标不变。

交付报告列出每个文件的真实路径、数量、CRS、已选 ID 或范围/标签、保留与丢弃字段、核验结果，以及失败范围、遗漏或无法组装对象。核验失败或部分下载要明确说明，不能把部分结果报告为完整数据。未经用户要求不额外简化、降低坐标精度、缓冲或融合要素。

在设置中启用 OSM 后，按用户任务直接下载，无需额外的会话下载确认；关闭 OSM 后停止提供下载。输出默认在本会话 output/osm。纠正同一次下载的参数后重下时，output 沿用原文件路径并传 overwrite=true，不通过新名称保留已确认错误的结果；不同数据仍用不同路径。下载后的异常要素修正先生成内存结果，再使用 data_export_vector 的 replace_source 指定原 GeoJSON，核验成功后原路径替换并刷新已加载图层，不另存 `_clean` 或 `_fixed` 副本。不能仅凭相似名称判断新旧结果的关系。凡部分结果、失败范围、无法组装的对象必须如实报告；clipped=false 时应做精确裁剪。OSM 行政边界不代表我国标准地图，国内行政区底图优先使用内置边界。

每份数据旁附 source.json，成果署名“© OpenStreetMap contributors”，数据许可 ODbL 1.0；共享派生数据库须保留相应许可义务。工具代码的MIT许可不能替代数据许可。
