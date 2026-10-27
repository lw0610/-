# GeoCode 来源与本地适配

参考项目：<https://github.com/zzhonglei/GeoCode-Release>。

核对日期：2026-10-06。执行帮助函数对照 GeoCode 源码发行版 v0.9.6，技能 manifest 版本为0.1.0，声明 MIT 许可。三份 `references` 保留上游完整章节和算法示例，附明确的本地兼容修改；本目录不是上游原样安装包，也不声明和上游所有行为相同。

本次固定参考资产：

- 技能：<https://raw.githubusercontent.com/zzhonglei/GeoCode-Release/catalog/core/gee-scripting/SKILL.d9a70d7b.md>
- 单日期模板：<https://raw.githubusercontent.com/zzhonglei/GeoCode-Release/assets/core/gee-scripting/templates/single-date-image.a015b1f1.md>
- 影像合成参考：<https://raw.githubusercontent.com/zzhonglei/GeoCode-Release/assets/core/gee-scripting/references/image-composite.0116bb60.md>
- 技能版本和许可：<https://raw.githubusercontent.com/zzhonglei/GeoCode-Release/main/contributions/gee-scripting/manifest/meta.json>
- 仓库许可：<https://raw.githubusercontent.com/zzhonglei/GeoCode-Release/main/LICENSE>，随本目录保留于 `LICENSE`。

完整参考固定到仓库提交 `6e3534f7af3675b19bd3c5d54c94022959dccdd2`，目录为 `contributions/gee-scripting/skill/references/`。下列 SHA-256 是修改前上游原文件的摘要，不是本地适配文件摘要；每个本地文件也保留相同来源注记。

| 本地文件 | 上游完整原文 | 原始行数 | 原文 SHA-256 |
| --- | --- | --- | --- |
| `references/image-composite.md` | [固定版本](https://raw.githubusercontent.com/zzhonglei/GeoCode-Release/6e3534f7af3675b19bd3c5d54c94022959dccdd2/contributions/gee-scripting/skill/references/image-composite.md) | 469 | `0116bb60978cb6c2ea55f9d6554f197a0aaed344c1362746918ccd0d14d8191f` |
| `references/classification.md` | [固定版本](https://raw.githubusercontent.com/zzhonglei/GeoCode-Release/6e3534f7af3675b19bd3c5d54c94022959dccdd2/contributions/gee-scripting/skill/references/classification.md) | 436 | `5b75a4d22643f8829193d3f44af927c034556459e06761a2f04f3d33fd0179dd` |
| `references/change-detection.md` | [固定版本](https://raw.githubusercontent.com/zzhonglei/GeoCode-Release/6e3534f7af3675b19bd3c5d54c94022959dccdd2/contributions/gee-scripting/skill/references/change-detection.md) | 355 | `a37168c01efacfdd8d9598924e860f1e3f45a1ac00c652f1360bfee0c39f62e6` |

许可和版本声明同样可按固定提交核对：[MIT LICENSE](https://raw.githubusercontent.com/zzhonglei/GeoCode-Release/6e3534f7af3675b19bd3c5d54c94022959dccdd2/LICENSE)、[技能 manifest](https://raw.githubusercontent.com/zzhonglei/GeoCode-Release/6e3534f7af3675b19bd3c5d54c94022959dccdd2/contributions/gee-scripting/manifest/meta.json)。原作者版权声明保留在本地 `LICENSE`。

采用的主要逻辑：本地可信区域、按任务选择时间与数据源、读取处理参考、有限候选日期筛选、同日拼接和像元云掩膜、全区域有效覆盖检查、达标提前退出、最佳候选报告、局部分块下载、阶段进度和输出核验。

必要适配：AutoCarto 已初始化 EE，无需 `from geocode import ...` 或 `geocode.init_gee`；由运行时提供辅助函数；下载沿用 geedim 分块与完整性检查、运行时超时控制和本地成果目录，并加载 QGIS。参考中的下载绝对占位路径改为相对文件名，结果和辅助文件统一归属 `output_dir`。区域读取沿用 GeoPandas 校验、重投影和单要素限制，无效几何不静默修复；不为此引入整套 geemap 依赖。分类训练样本可能包含多个点或面，改用文档内单独的 GeoPandas 读取函数，保留样本属性和原分类逻辑。

修正的上游接口不一致：v0.9.6 `geocode.check_coverage` 返回float，远程单日期及合成模板却使用 `report["coverage"]`；本地按float处理。日期分组保留影像类型、排序截断放在服务器；多波段使用共同有效掩膜；补充空候选、全无效检查以及不达标标识。单日期最佳候选有正有效覆盖即可下载并说明缺口，未达99%不再被本地额外门槛无条件阻断。未添加统一的固定检索天数或强制卫星/日期/用途三问。

其余明确的示例修正：合成参考将云量排序设为降序，使较低云量影像获得 `mosaic` 的后序优先权；分类参考将 `B8_entropy` 改为 Earth Engine 的 `B8_ent` 熵指标（[官方 GLCM 指标说明](https://developers.google.com/earth-engine/apidocs/ee-image-glcmtexture)）。变化检测完整算法予以保留，并显式提示共同有效掩膜要求，避免无观测区计入“未变化”；参数和模型精度仍需按实际任务验证。

本地交付适配：核验云端与本地结果时使用同一 ROI，不能把投影外接框中 ROI 外的正常空白误判为下载丢失。复用已完成且参数一致的成果；下载器负责分块，不通过生成手工南中北条带或扩大投影矩形绕过超时。估计坐标必须保留来源，不冒充用户明确指定。

超时语义对齐上游的输出活动检测：`gee_run_script` 使用配置的无 stdout/stderr 输出时限，默认300秒，允许1–3600秒。阶段心跳与下载器内建30秒心跳延长等待，用户停止及父任务截止仍生效；环境检查、云任务状态和取消短操作保留绝对时限。单会话精确请求复用是本地运行时适配：按影像表达式、region、scale、CRS、dimensions、dtype 匹配已完成且未变化的文件，跨进程同请求协调后返回原路径，不另复制；`reuse=False` 可显式重取。它不跨会话、不恢复未完成分块，也不覆盖旧版无登记清单的文件。

本地模板和参考文件随应用资产部署；运行时不依赖网络下载这些文档。只有数据处理本身需要 Earth Engine 网络和认证。

## GEE 工作流依赖核对与范围

以下逐项核对的是 GEE 取数及其所需的数据准备链，不代表移植了整个 GeoCode 应用。上游执行代码依据 v0.9.6，技能和数据依据上述固定提交。

| 环节 | 上游能力 | 本地对应与明确差异 |
| --- | --- | --- |
| 初始化与认证 | `init_gee` 使用配置项目和已有 EE 凭据 | worker 自动初始化同一官方 API；无需模型再次导入 `geocode`。查询、本地下载和区域读取分别报告就绪状态。 |
| 本地研究区读取 | `load_region` 检查文件、CRS、单要素、重投影、无效几何 | 保留这些检查，并限定 Polygon/MultiPolygon；不自动修复、不把城市换成估计矩形。 |
| 没有现成中国行政区边界 | 上游 `china-admin-boundaries` 提供天地图来源声明的完整省、市、县 WGS84 数据 | 原始数据现保存在 `autocarto/tools/system/admin-boundaries/assets/`，由 `admin-boundaries` 工具包提供精确选择入口，保留34省、375市、2891县共3300要素及固定源哈希。模板按唯一行政代码/名称导出单要素及来源记录；空 QGIS 工程也能准备边界。 |
| 有效覆盖 | `check_coverage` 返回 float，以整个 ROI 为分母 | 同口径；本地增加多波段共同有效掩膜，修正原参考将 float 当字典的问题。 |
| 单日期选取 | 按日期云量预选最多10个候选、同日拼接、99%早停、交付最佳候选并报告覆盖 | 主顺序保留；本地封装函数、服务器截断、空值检查、低云拼接顺序和输出报告是明确适配，99%不再成为额外硬性中止条件。 |
| 技能附件与样本 | 影像合成、分类聚类、变化检测三个完整参考及单日期模板 | 全部提供固定来源与许可；多要素分类训练样本改用保留属性的独立读取示例，避免误套单区域限制。 |
| 本地影像下载 | `geemap.download_ee_image` 调用 geedim 自动切片，完成后移动临时文件 | 直接调用固定 geedim 2.0.0；保留自动切片和异常清理，增加代理、实际进度、dtype/网格/字节完整性核验。没有 geemap 执行依赖；会话内完成文件复用为本地补强。 |
| 超时与取消 | 持续输出可延长空闲时限，上游默认为60秒 | 已对齐空闲语义，保留本地既有配置300秒；日志截断不影响续时。下载内建心跳，用户取消和父任务截止仍生效。 |
| 云导出 | `export_image` 提交并同步轮询 Drive 导出 | 本地明确采用提交、任务日志、状态查询、取消的异步工具；不把云端完成当作本地文件，不自动下载 Drive。 |
| 成果核验与加载 | `ReadGeoData`、QGIS 工具核验和继续处理 | 接续 AutoCarto 的数据检查及 `data_load_raster` 等 QGIS 工具；覆盖核验必须使用原 ROI，禁止因投影外接框空白重新下载。 |

相邻能力状态于 2026-10-27 更正：AutoCarto 已单独实现 `osm_search / osm_probe / osm_get_objects / osm_get_features` 工具链，来源、差异和当前边界详见 `autocarto/tools/system/osm-data/UPSTREAM.md`；不能继续表述为“尚未移植 OSM 工具”。上游独立 `osmfetch` 角色和完整技能商店未原样迁入，本项目仍按既有角色的工具包配置执行。中国行政区默认准备路径使用保留来源声明的上游数据包。

2026-10-27 使用说明补齐实际本地技能名 `gee-remote-sensing` 与可直接传给 `reference_read` 的包内路径，并保留上游按任务选择合成、分类、变化检测参考的方式。没有新增固定卫星、年份或重复确认门槛，未变更下载器、空闲超时、精确请求复用等运行时行为。

本轮行政区包已离线核验全部原始文件哈希、3300个多边形的几何有效性，并测试名称歧义、身份/CRS校验、源数据变更、输出保护及实际 `load_region` 读取。GEE 运行时和模板的针对性测试不等于已完成全部分类器、LandTrendr、CCDC 等参考算法的云端实验，具体科学任务仍需用其实际输入验证。
