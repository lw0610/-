# GeoCode 取数媒体与文件链路对齐

日期：2026-10-28。实施目录：`enio2`。本次针对用户要求按参考项目实现网页/浏览器取数，基线 `6c52948`。原 `Eino` 未修改。

## 依据与差异

核对 GeoCode-Release v0.9.6 的 MCP 结果附件、会话消息转换、`read` / `write` / `shell` / `webfetch` 实现，而非仅复制统计技能正文。官方源码 ZIP SHA256：`372d7d3a3813547c7d9101d2c7d96ec1e4b742c145f58d07622bba2c9280f8a4`。

统计方法来自固定提交 `6e3534f7af3675b19bd3c5d54c94022959dccdd2` 的贡献目录。随包原文未改动，SHA256 仍为 `9730863e36984c5fa4f64ed44485657eb4340f392e033ba35a907f83de59bb46`。来源、许可与适配说明见 `autocarto/skills/system/browser-data-acquisition/UPSTREAM.md`。

本项目保留 Eino 运行时、工具包分配、会话输出目录、来源登记与精确查询收据；没有把这些本地机制说成上游原有行为。也没有引入或承诺上游不存在的通用自动 OCR 引擎。

## 完成内容

- 浏览器 MCP 的真实图片和内嵌 PDF 保存为会话文件，经 `_agent_media`、增强工具、获取授权包装、ADK 进入模型。原来的授权包装会只保留文本，现保留完整媒体。文字截断不再吞掉后续附件。
- `web_read_page` 可以直接读取图片/PDF 原件，复用现有公开地址校验、下载容量限制、来源 URL、时间与 SHA256 清单。媒体不以 Base64 文字塞进工具正文；会话多模态结构保留完整字节以供恢复。
- 新增 `file_read`、`file_write`、`shell_execute`，复用现有文件操作包与内部工具路由，系统取数专家自动获得这些实际工具及说明。Plan 仅保留读取能力，不能调用写入和命令执行。
- 文件工具使用可信会话上下文；相对路径以会话 output 为基准。shell 调用本机已有程序，支持输出截断、超时和取消；它不是操作系统沙箱，也没有自动安装 OCR/PDF/Excel 环境。
- 模型能力检查移到统一模型工厂，覆盖获取专家、Plan 和普通角色。旧/新多模态字段、并行工具回复、Generate/Stream/WithTools 均处理；不支持媒体时明确返回能力说明，原件仍保留，不假称读到内容。
- 会话恢复及压缩保留原始多模态结构，避免提前清洗时丢弃新产生的媒体消息。`file_read` 不再被误认为生成文件，不覆盖已有来源或许可信息。
- 移除本地添加的 OCR 禁令；明确真实来源提取与无依据编造的区别。提示复用已下载原件、核对字段/单位/数值，并通过同次 `data_register` 交付；读不了时报告具体缺口，不反复截图或擅改年份。
- 追踪实际登记链发现 CSV 仍被送入只支持空间数据的 QGIS 规范化程序。现统计表复用 Go 的 CSV/TSV/XLSX 解析与核验，不要求几何或 CRS；已标准 CSV 保留原字节，字段别名只按声明复制。保留原件、成果哈希和原有登记门禁。

## 验证

1. `go test ./...` 全部通过。
2. `go test -race ./internal/fileops ./internal/acquire ./internal/dataservice ./internal/integrations` 通过。macOS 真实子进程组取消通过；Windows amd64 文件工具交叉编译通过，未做 Windows 实机测试。
3. 真实 SkillAdapter/Engine/ADK 测试覆盖媒体字节、增强工具授权、实际工具分配，以及恢复历史不丢附件。
4. 使用真实 SDK 和本地 HTTP 服务检查请求体：OpenAI 图片 data URI、Gemini 图片及 PDF inlineData，含原生流式请求与 WithTools。测试不调用公网模型。
5. `AUTOCARTO_BROWSER_SMOKE=1 go test ./internal/browserruntime -run TestPlaywrightSmoke -count=1 -v` 通过。固定 Playwright MCP 0.0.83 和真实 Chrome 隔离会话完成导航、快照、下载、页面表格导出及关闭；截图为可解码且非纯色的 1280×720 PNG（17,316 字节），未使用用户浏览器资料。
6. 真实跨包夹具执行原始表文本读取 → CSV 写入 → 登记 → 精确查询收据与哈希检查。空间工具执行器明确为 nil，不以假 QGIS 结果制造通过；其他查询借用收据、成果被篡改均被拒绝。该夹具的转录由测试提供，不代表模型 OCR 验收。
7. `node ./scripts/build-agent.mjs` 构建成功，macOS arm64、CGO_ENABLED=1；`git diff --check` 通过。此次没有修改前端页面，未重复前端全量测试。

## 实际运行与尚未验证的部分

构建后，已运行的 Tauri 开发监视器自动重启后端。22:50:34 启动的 PID 17947 使用 `enio2/autocarto-web/src-tauri/target/debug/carto-agent`；它与本次 sidecar 构建 SHA256 一致：`0b5093081454cb59455de211d2821dbc892efe249805383c4d81bc07d0aad71d`。HTTP 健康检查正常，runtime snapshot 的 Build/Plan 均 ready、stale=false、目录位于 enio2；实际工具目录已包含三个文件工具。

当前用户取数配置仍为 `openai-main / deepseek-v4-pro`，没有声明 `vision=true`。这说明当前配置未启用图片输入，不据此断言服务商实际模型永远不支持视觉。使用直接读图路径时，应选择实际支持图片输入的模型，并在模型设置声明其能力；数据获取设置中的“数据获取智能体模型”决定该角色使用哪个配置。本次未擅自更换模型或修改该声明。

当前安装的 Eino 适配器只有 Gemini 实现原生 PDF 输入。OpenAI、Claude、Ollama 的 PDF 路径会明确说明限制，可用本机已安装的提取/OCR程序先处理；不假称原生 PDF 功能已全部一致。

未重新执行用户完整河南人口专题图，未验证真实模型对人口图片的识别准确率、任意扫描 PDF 的提取成功率或最终制图效果。上述通过结论针对代码链路、真实本地浏览器、请求协议、文件登记和运行初始化。
