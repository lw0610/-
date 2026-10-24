# T52 前端子范围验收记录

日期：2026-10-21。工作树：`/Users/zw/研究生/QGIS-agent/enio2`。

## 范围与保留边界

按 T52、05 §6 清理前端旧用户概念与退役方法包测试，并修复完整 TypeScript 检查暴露的旧测试夹具。未修改 Go、工具资源、旧 Eino、运行数据或安装的 QGIS 插件。T52 总 removal ledger 由主代理合并。

`skill_packages` 仍只作为明确的 v1 配置输入兼容字段；v2 正常保存与界面选择使用 `tool_packages`。API 的 `/skill-packages` 技术路径沿用现有契约。普通聊天 `workflow_blocks`/agent_workflow 是消息过程投影，不是已下线的 SKILL workflow/v1 执行机制，因此保留其恢复与消息展示测试。`knowledge_engineer` 的负向角色测试用于证明角色 ID 不再特判，保留。

## 实施

- 诊断页将旧“技能”用户概念改为工具包，category 显示“工具包”，未知历史 kind 显示“已退役的包”；工具中心测试明确排除 deprecated category。
- 为方案全屏抽屉、新建角色对话框和工具中心四个筛选器补充名称与语义，页面测试按可访问名称查找，不再依赖第几个编辑按钮/筛选器。
- `tools-page.spec.ts` 原来保护的目录、筛选、详情、引用和启停行为，改由当前 category 工具包、四筛选、工具清单/使用说明/参考资料/引用情况覆盖；退役 accessibility-service-area 改为存续 vector-selection 的参考资料。恢复启停状态放入 finally。真实目录/API 失败会失败，删除其把 404 当成功的分支。
- `ui-state-repair.spec.ts` 原来“技能优先”断言改为通用角色类型、工具包搜索、系统包锁定、模型联动；保留聊天历史/任务/普通过程恢复。
- `system-smoke.spec.ts` 持久化断言改为 v2 `runtime_shared.tool_packages`，共享工具包选择跳过锁定系统包，验证公开 global catalog.items；临时方案在 finally 恢复原激活方案并删除。三文件统一使用共享测试端口，不再自行写死 3000。未把缺少真实后端的运行声明为通过。

## 原 12 处类型错误的修复

只修测试数据与测试调用，不放宽生产类型：

1. BackgroundTaskPanel SessionRecord 补 created_at/messages。
2. WebSocketProvider 取消帧补必需 content。
3. runtimeClientFallbacks mock 接收实际调用参数，调用记录可正确索引。
4. sessionAttachReplayRequired 删除 Pick 契约之外的 content。
5. useChatMessaging 删除已移除的 sessionPanelCollapsed。
6. useChatSession 删除同一旧字段。
7. sessionReattach 删除 expect 的无效第二参数。
8. settingsRegistry 夹具明确 SettingDescriptor 类型，补 impact/mutability。
9. release-candidate RuntimeSnapshot 明确类型，初始 active_id 使用 undefined，并将运行实例明确为 QGISInstance，避免空数组/普通 string 的错误推断。

以上位置覆盖初始检查的 12 条诊断（单文件可能多条）。

## 实际验证

- `npm run lint`：通过，0 warning/error。日志 `/tmp/enio2-t52-lint.log`。
- `npm test`：74 文件、335 测试全部通过。日志 `/tmp/enio2-t52-tests.log`。
- `npx tsc --noEmit --pretty false`：完整项目检查通过，包含测试夹具，未排除旧错误。日志 `/tmp/enio2-t52-full-tsc.log`。
- `npx playwright test --config playwright.profile.config.ts`：真实 Chrome 1 项通过（11.7s），覆盖 v2 角色编辑、系统包锁定、执行器保留、v2 保存、流程跳转和能力手册。日志 `/tmp/enio2-t52-profile-playwright.log`。该配置的 HTTP 是契约 fixture，不是 Go/QGIS 端到端。
- 前端相关文件 `git -c core.whitespace=cr-at-eol diff --check`：通过。

三项旧页面 E2E 已改写并包含在完整 tsc；其真实服务执行留给 T53 隔离后端联调和主代理统一 check。此记录不把静态编译或契约回放当作真实 QGIS 验收。
