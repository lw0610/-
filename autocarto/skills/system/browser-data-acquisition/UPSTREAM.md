# 网页与浏览器取数：来源与本地适配

参考项目：[GeoCode-Release](https://github.com/zzhonglei/GeoCode-Release)。核对日期：2026-10-27。

## 中国统计数据方法

`china-statistics-data` 来自仓库 `contributions/china-statistics-data/skill/SKILL.md`，不是 v0.9.6 应用源码 ZIP 内的文件。此前说明将“源码 ZIP 中未找到”当作没有可核对技能的依据，现已更正。

- 固定提交：`6e3534f7af3675b19bd3c5d54c94022959dccdd2`
- [固定原文](https://github.com/zzhonglei/GeoCode-Release/blob/6e3534f7af3675b19bd3c5d54c94022959dccdd2/contributions/china-statistics-data/skill/SKILL.md)
- 原文 Git blob：`e27226ad75eeda50819ad9522dc219ff391629a7`
- 原文 SHA-256：`9730863e36984c5fa4f64ed44485657eb4340f392e033ba35a907f83de59bb46`
- 未修改原文随包保存在 `upstream/china-statistics-data.md`；完整中文适配为 `references/china-statistics-data.md`。
- [同提交 MIT 许可](https://github.com/zzhonglei/GeoCode-Release/blob/6e3534f7af3675b19bd3c5d54c94022959dccdd2/LICENSE)，版权与许可保存在 `LICENSE`。

保留的上游逻辑：Playwright 操作官方 JavaScript 网页；完整导航与七个地区入口；按频率、地域与指标选择数据；“序列”批选省份；确定指标、时间、地域和表格形态后整理 Excel；缺失值先查国家统计局其他页面，再查可信补源；来源、单位及定义差异随成果交付。

本地适配：通过 `browser_list_tools` 和 `browser_call_tool` 接入 Playwright MCP；启用入口改为本项目扩展服务设置。已在用户需求或流程中明确的条件直接复用，避免重复问答。按实际页面核查“序列”和导航控件；优先原始导出，必要时提取可见表格并保留来源。独立取数保留上游默认 Excel 交付；制图流程明确要求 CSV 时，以真实 UTF-8 CSV 和来源记录完成该取数步骤，保留已经下载的 Excel 原件，不因缺少额外 Excel 转换能力阻止合格 CSV 继续制图。此处是面向本地流程交接的格式适配，不宣称 CSV 就是 Excel。

页面表格导出可使用当前固定 Playwright MCP 0.0.83 的 `browser_evaluate`，或实际工具列表提供的等价能力，通过页面 Blob 触发 CSV 下载；必须以 `browser_list_tools` 的真实 schema 为准，不能假定旧 `browser_run_code` 名称可用。只整理实际呈现并完整取得的表格，不绕过网站接口；未取得真实下载路径时不声称文件已交付。浏览器不可用时只阻止依赖浏览器的取数操作，不禁止继续其他独立任务。

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

本次核对和测试验证技能可发现、方法可读取及工具调用契约，不代表已在线逐一验证国家统计局全部入口或完成真实统计文件下载。页面与服务变化应以每次工具返回为准。
