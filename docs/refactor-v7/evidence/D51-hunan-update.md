# D51 湖南河流与铁路：本轮真实模型回放

2026-10-29。**河流、铁路和GeoPackage真实成果已经完成；PDF未完成，最终运行状态为interrupted，不计完整出图通过。** 没有手改模型计划、回执或运行状态，没有代替模型调用制图工具。

## 环境与记录

- 隔离目录：`/private/tmp/autocarto-v7-d51-hunan-update-g82gtg_v`，端口50127，真实QGIS离屏工程；没有改用户工程或原会话。
- 后端：`/tmp/enio2-v7-update-agent`，实际SHA-256见验证JSON；使用当时用户配置的副本，仅重定位data_dir、log.dir。未切换模型或更改工具预算。
- 会话：`c88fcf36-900c-4673-83fc-0850796b7db5`；计划：`cp-20261029-190732-5132c1296d74`；运行：`fr-20261029-190746-1181beca563f`。
- 请求：湖南省河流线、铁路线用于A4横向省级图，真实可核验来源、中文名称、按比例尺取舍，加载工程、导出GeoPackage，并制作A4横向PDF。
- 首轮：`replays/hunan-v7-updated-20261029-190706`；同会话继续：`replays/hunan-v7-continue-20261029-191606`。目录中保留request、events、plan、summary、agent/QGIS日志。

| 阶段 | 耗时 | 模型工具调用 | 失败工具 | 结果 |
|---|---:|---:|---:|---|
| 首轮 | 493.66秒 | 42 | 2 | 边界、铁路、河流已准备和裁剪；模型发出纯“本步完成”确认后暂停 |
| 确认继续 | 395.85秒 | 52 | 11 | GeoPackage完成；排版恢复受阻，最终模型服务402余额不足 |

两轮合计889.51秒。按call_id跨轮去重为93个模型工具调用，13个失败；同一确认工具在恢复时再次出现，不能简单把42和52相加。第二轮有25次shell调用。现有记录没有精确模型请求计数，不把工具次数当模型次数。

## 实际过程与问题分类

1. 首提计划遗漏河流、铁路的extent，预检needs_data；模型自行plan_revise补“湖南省”后v2 ready，没有人工改计划。此场景不能计入首提成功。
2. 隔离环境准备只复制了旧缓存3个目录及manifest，没有复制数据仓index.json，故没有命中预置缓存。**这是本次回放准备失误，不是程序缓存失效证据。** 本轮实际联网下载湖南省51,162,608字节原件；铁路准备350秒，包含下载及规范化，河流准备24秒并复用此次下载原件。旧缓存源文件SHA-256逐项检查未变。
3. 河流裁剪完成后，模型调用hitl_request，内容为`{type:plan_confirmation,title:本步完成,description:裁剪河流线至湖南省已完成,select_mode:confirm}`，没有选项或改变数据的决定。按已授权隔离验收范围只确认继续一次，未选择替换数据或接受失败。该额外确认仍是不必要的停顿。
4. 测试脚本遇到未匹配的确认会终止QGIS，恢复时重新打开项目。原`hn_boundary`是memory图层：同ID从1要素变为0，extent为空；河流与铁路文件层保持可读。首轮模型已额外导出有效`hn_boundary.geojson`，但继续后的排版角色未正确载入该文件。这是本次测试暂停重启触发的内存层丢失，不能说没有中断地完成，也不能把它冒充原取数失败。
5. 导出阶段还有独立接线错误：`package_layers`返回缺少provider `unknown`；`save_vector_features_to_file`两次返回“未知工具: native:savefeatures”。另有合并LAYERS参数错误。模型最终通过本机GDAL生成真实GeoPackage，控制器第4步completed；这些工具错误不能因最终文件存在而忽略。
6. 排版阶段尝试把GeoJSON路径当工程图层执行筛选、对空内存层选择；随后多次shell读取/尝试修改QGZ，遇到缺少shapely和`AssertionError: tree source not found`。这是无效恢复绕行，尚未得到完整版面或PDF。
7. 最终收到`402 Payment Required / Insufficient Balance`，request_id为`55344723-56d5-4578-8266-0e1d345cb09e`。控制器保留前四步completed，第5步draw_map interrupted、outputs为空。之后停止所有模型调用，未自动重试或换模型。余额不足是最后停止原因，不能掩盖前面的接线错误与恢复问题。

## 真实文件只读核验

目录为上述会话的`exports/`：

| 文件 | 实际内容 | 核验 |
|---|---|---|
| hn_boundary.geojson | 湖南省1个面 | GeoJSON可解析 |
| hn_boundary_reload.gpkg | 模型恢复时生成的1个面 | SQLite integrity_check=ok；未据此声称已成功重载工程 |
| hn_railway_clip.gpkg | 1,227条MultiLineString | integrity_check=ok，EPSG:4326 |
| hn_river_clip.gpkg | 370条MultiLineString | integrity_check=ok，EPSG:4326 |
| hunan_gpkg.gpkg | railway 1,227条、river 370条 | 724,992字节，integrity_check=ok，均EPSG:4326 |

具体SHA-256、完整路径、去重工具失败、准备台账和最终步骤状态见 [D51-hunan-update-verification.json](D51-hunan-update-verification.json)。文件数和几何类型不等于已完成地图视觉验收；本轮没有PDF。

## 认证副本清理

回放结束后只删除本次新建隔离home内的`config/secrets.enc`和`config/.master_key`。其余配置扫描未发现非空明文认证字段；配置、脱敏事件、日志和产物保留，清理记录在隔离home的`credential-cleanup.json`。用户原始配置及旧隔离回放认证文件未动。

本记录描述的是修复前实际发生的错误。后续两项导出工具的本地QGIS修复验证另记，不能据本地测试把本次模型运行改写为完成。
