# K50 — 指南、API 与旧编译器清理

## 已实施

- 删除 FLOW 编译器、模型抽取、增强缓存/后台队列、能力检索配角和固定表单输入/条件执行辅助；只保留迁移用声明解析及历史 JSON 解码。
- 删除旧 `/flows` 管理、编译、输入校验、角色对应接口；指南使用 `/guides`。资源 API 与注入的指南仓库共享同一包存储，引用保护不会查错目录。
- `PlanEnvironment` 只读取本地目录、权限、配置与模型能力元数据，不创建模型；角色快照包含实际 Provider，供计划选角验证读图能力。
- `FlowPolicy` 活跃定义、校验和克隆删除。旧 `flows`、`flow_policy`、独立 `plan` 配置读取后忽略，保存移除；旧文件备份仍保留。
- 旧 source-choice 解码移入 `flow/legacy`，仅供旧记忆迁移；新运行使用数据服务结果和显式文件/图层引用，不解释旧表单标记。
- 保留资源修复、导入导出、只读内置资源、原子失败回归；资源指纹增加实际样式/版面入口文件检查，暂停后变更需重新确认。
- 旧数据获取验收迁移为 GuideStore + dataservice.Find/Get，保持真实 QGIS 加载、样式、来源、字段、缓存及原文件复用检查。

## 验证

- `go test ./internal/flow/... ./internal/api/... ./internal/config/... ./internal/settings/... ./internal/control/... -count=1`：全过。
- `AUTOCARTO_QGIS_PYTHON=/Applications/QGIS-LTR.app/Contents/MacOS/python go test ./internal/api -run '^TestBuiltinAcquisitionRealQGIS$' -count=1 -v`：通过，自动获取 8 层 + 补齐 8 层，使用本地假服务器，真实 QGIS 核验。
- 指南 CRUD、启用、复制、导入导出、资源管理保护回归保留；GUIDE 路由支持 OPTIONS。

## 边界

- 此记录覆盖 flow/API/config/settings 泳道。控制器、WebSocket、前端和完整端到端验收由其他 K50/K51/K52 记录覆盖。
- 未修改用户会话、真实用户流程或用户配置；迁移测试仅操作临时目录。
