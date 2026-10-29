# D51 郑州地铁：408 秒暂停的实际原因与定向修复

2026-10-29。读取既有真实回放，不重新调用模型，不改原 home、缓存或会话。这里的修复验证不代表整张地铁覆盖图已完成。

## 现场与时间

home `/tmp/autocarto-v7-d51-metro3-unrslx9l`，session `e36cd436-2574-4e14-90ae-425349091a34`，run `fr-20261029-154408-183d6cdfb5f4`。
事件文件 `replays/zhengzhou-metro-800m-resume-20261029-155904/events.jsonl`。

只统计 `agent_workflow`，不重复计算镜像 `run_event`：数据工程师尝试 61 次工具，实际领取 60 次，第 61 次 `output_get_info` 被预算阻止；其中 `shell_execute` 23 次，回执 `duration_ms` 合计 **791 ms**，3 次非零退出。
run.json 第 1282 行记录该步 **408 秒**，第 1297 行是 `EXECUTION_BUDGET_REACHED`。408 秒不是这些脚本的计算时间，也不是此阶段在等下载；主要耗时来自大量模型生成/工具往返轮次，现有回执不足以把每一秒严格分解成模型与调度延迟。

## 已确认的两个程序问题

### 1. advertised 最近邻工具实际不可执行

- events 第 344 行调用 `join_attributes_by_nearest`；第 346 行返回 `provider_missing`、provider=`unknown`。
- 原 YAML 的 `backend_tool: join_attributes_by_nearest` 不是 QGIS 注册算法。旧 D51 记录“调用了空间工具”只能证明可见，不能证明调用成功。
- 本机 QGIS 3.44.10 的真实算法是 `native:joinbynearest`，参数包括 INPUT、INPUT_2、FIELDS_TO_COPY、NEIGHBORS、MAX_DISTANCE、OUTPUT；所有公开参数均核对真实 registry。
- 仅修 ID 后，真实测试又证实 generic processing 包装只返回 `str(result)`，没有把 memory 输出加入工程，无法得到下一步可用的图层。
- 最终实现将 YAML 绑定到 `native:joinbynearest`，并统一修正 Processing 执行器的结构化输出与图层发布。中途新增的专用 nearest handler 已删除，最近邻与其他 Processing 工具共用现有执行链，返回 layer_id 和原生匹配计数；详见 [公开工具派发修复](D51-processing-dispatch.md)。

真实 QGIS 回归：memory 与 GeoPackage 两种输出，最近点距离 5 米正确；10 米外要素保留且连接字段/距离为空；只复制指定字段；源层未改变。`test_join_nearest_qgis.py` **2/2 通过**。
既有字段处理模块 **3/3 通过**；工具迁移契约 **1/1 通过**。Go toolcontract/toolcatalog、真实 builtin schema/registry 目录检查通过。

### 2. 146 个真实站名在规范化中被丢弃

原件：`data/acquired/source.osm.overpass/aa3bf16a9be89df7/raw/features.geojson`。

| 项目 | name 非空 | name:zh 非空 | 总要素 |
| --- | ---: | ---: | ---: |
| 原始下载 | 239 | 93 | 240 |
| 旧规范化结果 name | 93 | — | 240 |
| 本次临时重算结果 name | 239 | — | 240 |

原始“医学院”记录实际 `name=医学院`、`name:zh` 为空。QGIS 返回空值类型为 `QVariant`，显示 `NULL`；`QgsVariantUtils.isNull(value)=True`，旧 `_present(value)=True`。旧实现只排除类名 `QPyNullVariant`，因此候选 `['name:zh','name']` 在空中文名称处提前结束，没用已有普通名称。

修正仅在 `_present` 入口使用 `QgsVariantUtils.isNull`，保留 0/False 的有效值语义。不猜站名，不补造字段。
真实原件原样复制为 `autocarto/tests/fixtures/metro-stations-osm.geojson`（64,265 字节，ODbL 来源说明同行保存），测试验证 239 名称保留、1 条原本完全空；另测中文优先、中文空时回退、完全空，以及 0/False。原件字节保持一致。
在与标准门禁相同的 QGIS Processing/GDAL 路径环境下，`test_data_acquisition_qgis.py` **10/10 通过**。首次独立命令遗漏 GDAL_DATA 导致既有 OSM 输入测试找不到 osmconf.ini，补齐运行环境后全通过；没有为测试改动生产行为。

旧 home 的缓存未修改；直接恢复旧计划仍会读旧的缺名产物。后续真实验收须通过正常数据规范化/缓存更新流程重建该项，不能把本次临时重算偷偷覆盖成旧现场的成果。

缓存接线只读检查发现：`CacheSpec.Normalize` 参与完整 key，但此前只有字段/过滤/几何等参数，没有实现版本；`baseKey` 则有意移除 Normalize 以复用原件，`FindSemantic` 此前绕过 Normalize 比较。取数代理随后已完成针对性修复：只对多候选字段添加 `Normalize.revision=field-candidates-v2`，语义匹配也排除该类旧处理结果；旧原件仍可复用，单字段缓存不失效。真实 fixture 的旧 93 名结果排除、零下载重算、之后新缓存复用及 acquire/dataservice 检查通过，详见 [规范化缓存版本证据](normalization-cache-revision.md)。旧会话已完成台账仍不自动改写。

## 其他已发生的低效行为与判断边界

- 第 182 行开始 shell 查属性，早于第 344 行最近邻失败；所以最近邻接线错误不是全部原因。
- 第 209/211 行 SQLite `ST_MinX` 不存在；213/217 行外部 Python 缺 `osgeo`；220/224 行把 GeoPackage 二进制几何当 JSON。
- 第 353/355 行确认外部 Python 同时没有 geopandas/shapely/fiona/pyproj。随后多轮手写 GeoPackage/WKB，出现长度全 0、点数全 0；第 406/408 行终于得到数字，但使用“最近顶点”与度数乘 111 估算，不能替代真正的最近线距离与投影量算。
- QGIS `field_calculator` 与 `extract_by_location` 实际成功；现场角色没有 `pyqgis-scripting`，但不能说所有空间能力都缺失。需要几何计算时应使用已有 QGIS 工具或明确授予 QGIS 内部脚本能力，避免外部 Python 从二进制重新实现 GIS。
- 当前交付线路 raw `zhengzhou_metro_lines.geojson` 的 791 条全部是 OSM way：160 有名称、631 没名称；归一为 775 后仍 631 无名。先前原始确定性查询另含 relation，但专家重取的这份没有。不能把该结果说成“关系与 way 混合重复”。无名轨道分段本身不证明数据错误，不能仅靠最近名称猜其运营状态。
- 现场站点归一结果仅保留 name，不能从该表核验原有 station/public_transport 等来源标签；原件尚保留这些。先修名称丢失并保留必要来源字段，再评估真实运营条件。暂未新增完整性门槛或自动删站规则。

最小职责建议：先保证已有工具真正可调用及字段不丢失；GIS 几何交给 QGIS，来源/运营语义从原始属性或明确来源核验；不要以增加预算掩盖重复写解析器。是否完整成图仍需后续真实回放，未在本次宣称通过。
