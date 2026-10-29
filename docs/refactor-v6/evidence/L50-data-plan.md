# L50 数据与计划分层验收

日期：2026-10-28。与主代理的 L51 总记录合并阅读。

## 覆盖边界

本记录把真实后端逻辑、脚本模型边界与浏览器契约回放分开。没有将预设工具结果称作真实公网下载或真实大模型理解，也没有操作用户运行目录。

| 标准 | 真实后端证据 | 浏览器证据 |
|---|---|---|
| Z3 用户给了就用 | `TestL30LiteralPathAndAttachmentBoundWithoutPrefix` 绑定人类原文中的真实临时路径与附件/@元数据，不扩大到同名前缀目录；`TestL30ExplicitUserAndProjectPlanDoesNotAcquire` 实际准备计划，验证用户原路径、工程ID不变，执行步骤不插 acquire；当前 @ 优先与历史附件不跨需求误配有独立回归 | plan-data-v6 显示“您提供”“工程图层”，原路径和图层名称准确 |
| Z4 没给才获取 | `TestGetVerifiedUserBindingAndAutomaticNeed` 先直接使用原件且工具调用为0，再移除用户来源并选择真实 local adapter 经引擎获取，核验来源记录与会话索引；HTTP/OSM只用本地测试服务器，验证真实引擎、缓存、许可/attribution/摘要 | 自动获取行显示候选名、范围与比例尺理由，获取步骤只引用缺失的铁路，完成后显示成果 |
| Z5 文件夹对应 | `TestL30ProvidedFolderMatchesHeadersWithoutWork` 真实 GeoJSON 名称/几何匹配，原件字节未变且未调用执行器；shp/gpkg只读头部匹配；弱匹配进入计划确认；明确单文件错误不偷偷转下载 | 弱匹配行醒目“请确认”，与其他来源并列显示 |
| 工具契约 | 真 YAML→Tool→registry→adapter→Eino 的嵌套schema测试；真实ADK脚本模型连续第三次坏参数后停止（3次模型请求，3次失败观测）；选定流程全文注入及skill实际参数名验证 | 与模型调用结果配对改动由根代理独立验证 |
| 计划编辑 | 同一 find_id 不同候选按data ID分别保存；显式从候选改为用户已有文件时保持from=user，不产生获取；切换工程层清除旧find_id避免旧来源约束误伤 | 现有PlanCard单测/回放覆盖操作契约 |

## 运行记录

- `go test ./internal/dataservice ./internal/plan ./internal/planexec ./internal/flowrun ./internal/skill ./internal/agentruntime ./internal/websocket -count=1` 七包通过（末次 2026-10-28）。
- `go test ./internal/dataservice ./internal/tool ./internal/toolcontract -count=1` 通过。
- API 三方案 × 四流程 × 用户/获取来源矩阵及计划几何拒绝测试通过。
- 主代理统一 Playwright：**41 passed / 4 skipped**，约1.1分钟。新增 `tests/e2e/plan-data-v6.spec.ts` 通过；真实HTTP额外两项由主代理开启LIVE运行通过。

新增浏览器回放：一句话 → 来源/样式/请确认 → 开始（版本1）→ 三步进度 → 成果；只模拟 HTTP/WS 契约，不连接真实模型、网络或用户 QGIS。截图为 [L31-plan-sources.png](L31-plan-sources.png)。真实QGIS新流程/资源图件验收由另一泳道记录，不能由这个浏览器回放替代。
