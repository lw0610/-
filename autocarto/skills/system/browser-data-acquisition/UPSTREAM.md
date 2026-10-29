# 网页与浏览器取数：来源与本地适配

参考项目：[GeoCode-Release](https://github.com/zzhonglei/GeoCode-Release)。核对日期：2026-10-28。

## 中国统计数据方法

`china-statistics-data` 来自仓库 `contributions/china-statistics-data/skill/SKILL.md`，不是 v0.9.6 应用源码 ZIP 内的文件。此前说明将“源码 ZIP 中未找到”当作没有可核对技能的依据，现已更正。

- 固定提交：`6e3534f7af3675b19bd3c5d54c94022959dccdd2`
- [固定原文](https://github.com/zzhonglei/GeoCode-Release/blob/6e3534f7af3675b19bd3c5d54c94022959dccdd2/contributions/china-statistics-data/skill/SKILL.md)
- 原文 Git blob：`e27226ad75eeda50819ad9522dc219ff391629a7`
- 原文 SHA-256：`9730863e36984c5fa4f64ed44485657eb4340f392e033ba35a907f83de59bb46`
- 未修改原文随包保存在 `upstream/china-statistics-data.md`；完整中文适配为 `references/china-statistics-data.md`。
- [同提交 MIT 许可](https://github.com/zzhonglei/GeoCode-Release/blob/6e3534f7af3675b19bd3c5d54c94022959dccdd2/LICENSE)，版权与许可保存在 `LICENSE`。

保留的上游逻辑：Playwright 操作官方 JavaScript 网页；完整导航与七个地区入口；按频率、地域与指标选择数据；“序列”批选省份；确定指标、时间、地域和表格形态后整理 Excel；缺失值先查国家统计局其他页面，再查可信补源；来源、单位及定义差异随成果交付。

本地适配与差异：

- 通过 `browser_list_tools` 和 `browser_call_tool` 接入 Playwright MCP；启用入口改为本项目扩展服务设置。浏览器操作遵守真实 schema 与当前页面。沿用用户或流程已明确的条件，只有确实缺失或有歧义时询问。
- 独立取数保留上游默认 Excel 交付；制图流程明确要求 CSV 时，以真实 UTF-8 CSV 和来源记录完成取数步骤。已下载原件保留，不额外强制生成 Excel，不以改扩展名冒充转换。
- 本项目统一由数据获取智能体执行，以 `data_find` / `data_get` 建立查询，最终以同次查询调用 `data_register` 核验成果。这是本地流程交接机制，上游技能没有冻结 Selection 或登记接口。登记的是符合后续任务格式的实际成果；图片或 PDF 可以作为来源原件，读取整理后再登记统计表。
- 文件读取、写入与命令执行能力对齐 GeoCode v0.9.6 的 `read`、`write`、`shell` 工具分工，本项目名称为 `file_read`、`file_write`、`shell_execute`。下载原件后读取实际内容，必要时通过视觉转录、文档提取或已安装的 OCR/表格工具整理，核对并写出真实成果。原先本地添加的截图 OCR 禁止已移除；有来源依据的提取不等于无来源编造。工具是否可用、附件是否被模型接收、模型是否支持该媒体、命令与依赖是否安装，都应以真实结果为准。

本地文件工具按会话组织成果：相对路径以当前会话输出目录解析，`file_read` 按会话、工程与绑定图层的可信范围读取，`file_write` 只写当前会话输出目录。`shell_execute` 在输出目录运行本机命令，不附带新的 OCR/文档运行环境，也不声称是命令沙箱。这些是本地运行边界，不应描述为 GeoCode 原文的要求。

页面表格导出可使用当前固定 Playwright MCP 0.0.83 的 `browser_evaluate`，或实际工具列表提供的等价能力，通过页面 Blob 触发 CSV 下载；必须以 `browser_list_tools` 的真实 schema 为准，不能假定旧 `browser_run_code` 名称可用。只整理实际呈现并完整取得的表格，不绕过网站接口；未取得真实下载路径时不声称文件已交付。浏览器不可用时只阻止依赖浏览器的取数操作，不禁止继续其他独立任务。

上游 `china-statistics-data` 是取数方法说明，要求把真实网页数据整理为工作簿，没有提供或承诺完整的图片、扫描 PDF、OCR 自动处理管线。这里补充的是本项目从原件读取到成果登记的工具衔接，不把任意图片/PDF 的可读性、识别准确率或任务成功当作保证。无法读取时应保留原件并报告能力缺口，不重复截图冒充读取，也不默认改变用户的年份、指标或统计口径。

该方法附属现有 `browser-data-acquisition` 工具包，通过包描述发现、`skill` 读取主说明、`reference_read` 按需读取完整方法。不会新增需要用户逐角色手动挂载的统计技能。

## 通用网页获取

调用顺序参照 GeoCode v0.9.6 的 `WebSearch`、`WebFetch` 与 Playwright MCP 分工：未知来源先搜索；已知地址直接读取；交互式页面由浏览器操作。搜索服务由设置选 Exa 或 Parallel，使用其 MCP 协议；不自动替用户更换服务商。公开文件下载是本项目单独提供的工具，保留当前会话路径、来源清单、校验值与不覆盖行为。

对应源码位于发行包 `packages/opencode/src/tool/websearch.ts`、`mcp-websearch.ts`、`webfetch.ts`。上游无显式覆盖时按会话校验值在 Exa/Parallel 中分配；本项目保留显式设置选择，默认 Exa，未移植该分配策略。两者都不是请求失败后自动切换服务商。搜索协议、参数与实际服务探测由 `carto_agent/internal/webdata` 实现和测试；本工具包只承载发现与操作方法。

上游源码 SHA-256：

| 文件（位于 `packages/opencode/src/tool/`） | SHA-256 |
| --- | --- |
| `websearch.ts` | `edb175726b7830d242f59417ce6961f44d39e500bccc068bf2dddbf61d5ca92a` |
| `mcp-websearch.ts` | `44cec72f6a8995ff172bc774e4a2e689fd1aa77fb9cfb9575d66a0e4bab24a88` |
| `webfetch.ts` | `41c908b719fc0a95a6ed52c1749f8dc69161c8cfc35eff5f8ae4f610358c4ed5` |

核对和测试覆盖技能可发现、方法可读取及工具调用契约，不代表已在线逐一验证国家统计局全部入口、任意图片/PDF 的提取或完成真实统计制图任务。页面、服务、模型媒体支持及本地依赖变化应以每次实际结果为准。
