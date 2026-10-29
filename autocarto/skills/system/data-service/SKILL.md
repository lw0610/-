---
name: data-service
display_name: 数据服务
description: 统一查找数据来源、获取并核验数据、登记来源。
kind: category
domain: 数据
system: true
hidden: false
enabled: true
auto_mount: ''
tools:
  - data_find
  - data_get
  - data_register
---

# 数据服务

需要数据时先查 `data_find`，说明类别、范围和必要字段；已有用户文件、工程图层优先。查询本身不联网，推荐与排除理由来自本地目录，不自行编造候选编号。编排智能体只有查找能力，可把结果写入制图计划，或委派系统数据获取智能体。

独立取数时，数据获取智能体使用 `data_get` 选择并绑定本次查询，选择映射的星号键可设为 recommended。若返回 `exploration_required`，按 `preparation` 中的同次查询与可用兜底途径取数。计划执行器已传入冻结 Selection 时，查找和绑定已完成，直接沿用，不重复 `data_find` 或 `data_get`。缺少范围时先用已知区域、已有数据或上游步骤产出补齐，不反复盲试在线工具。每项数据分别查找，用户文件夹通过 provided 自动匹配，用户原文件保持只读。获取结果与来源说明登记到当前会话；用户明确选择的路径与来源可以记住，数据值通过数据仓复用。

当前统一服务尚未接入在线瓦片加载前的独立授权：天地图瓦片连接定义及其缓存不会作为可用数据候选。加载此类服务需要用户明确同意密钥写入 QGIS 工程，不能用普通联网确认代替；不要把图层定义 JSON 当成已下载或已加载的数据。可以使用已下载且核验通过的栅格文件，或当前工程中已经存在的真实图层。

只有没有可用的确定性来源且设置允许时，才使用自定义 OSM、门户或联网搜索兜底。`data_find` 设置 `include_fallback=true`；`state` 区分 `available`（已有）、`acquirable`（可直接获取）、`exploration_required`（需探索）、`needs_input`（补需求）与 `unavailable`（无可用路径）。前两种及真实探索来源均可写入计划，后两种先处理阻塞。每次真实下载后必须 `data_register`，提供冻结的同次查询、行名、路径、真实网址与许可；核验通过后才可供后续步骤使用，不新建查询来登记旧文件。遇到验证码或登录请求让用户处理，不绕过限制，不生成或虚构数据。

## 参数与来源

`data_find` 只查地图数据来源，不能查找流程文档。优先写描述，what 必须是对象：

```json
{"need":{"name":"河流线","what":{"description":"线状河流，需要河名"},"geometry":"line","extent":"用户指定的区域"},"provided":[]}
```

用户给文件或目录时把其实际路径放入 `provided` 数组。程序读取 GeoJSON、Shapefile、GeoPackage 头部与字段，匹配不写原文件。几何相同但名称较弱的候选标记 `requires_confirmation`。`what.list` 已移除；每项需求独立查询。用户或工程的候选在计划中标记 `from=user/project`，不要安排获取步骤；对应不上才使用自动获取。

类别已识别但参数缺失或非法时，返回 `DATA_NEED_PARAMS_INVALID`，`resolved.category` 保留已识别类别，`diagnostics.parameters` 列出需要修正的 `need.params` 路径、必填性和允许值。按目录约束和已知需求一次补齐，不猜默认值；若用户意图不足以选择，先澄清。仅类别未识别时才根据类别建议调整 `what`。

JSON 或结构参数错误会返回正确示例，连续三次此类错误停止本轮，不得换智能体继续盲试。当前这个计数不覆盖类别参数值等语义诊断；遇到语义诊断同样必须先按返回约束修正，不能原样反复调用。

用户文件夹中的候选用于计划时保留 `find_id` 与 `choice`，同时写 `from=user` 和候选真实 `path`。GeoPackage 有子图层时 path 使用 `文件.gpkg|layername=子图层名`。本机已有数据不会产生获取步骤，弱匹配会在计划卡等待确认。
