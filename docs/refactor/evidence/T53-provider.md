# T53：会话订阅变化导致连接重建的修复与回归

日期：2026-10-21。仅在新项目 `enio2` 修改 `WebSocketProvider.tsx` 及其现有测试；原项目未修改。本条是最终交付审查发现的确定问题，真实页面最终结果由 [T53-ui](T53-ui.md) 汇总，完整最终门禁由 [T53](T53.md) 汇总。

## 已确认的问题

连接生命周期 effect 原依赖 `syncSessionAttachments`，而该 callback 依赖 `activeSessionId`、`chatSessionId`、`runningSessionIds`。切换会话、改变运行集合或处理 `flow.finished` 时 callback 改变，触发连接 effect 清理，执行 `disconnect()` 并重新初始化连接。真实页面曾出现流程已完成、同时显示后端断连提示；该截图和页面验收记录归 T53-ui。

新增两项回归后，先在尚未修复的实现上执行：

```sh
cd autocarto-web
npm test -- src/components/providers/WebSocketProvider.test.tsx -t 'updates session subscriptions|uses the latest session targets'
```

结果：退出码 1，两项失败。第一项切换活动会话后未按现有连接发送新会话 attach；第二项在离线期间改变会话集合、再次连接后，`connect` 实际调用 2 次，期望 1 次。不是仅依据依赖列表推测。

## 修改

新增 `syncSessionAttachmentsRef`。已有独立订阅 effect 在会话集合变化时更新 ref 并继续同步 attach/detach；连接回调通过 ref 获取最新订阅逻辑。连接生命周期 effect 移除该易变 callback 依赖。

没有修改消息协议、事件序号、回放规则、任务重试、后端执行或流程状态机。卸载仍断开连接，启动失败重试仍通过原 `retryToken` 运行。离线后再次收到连接通知时仍使用最新会话目标。

## 实际验证

```sh
cd autocarto-web
npm test -- src/components/providers/WebSocketProvider.test.tsx src/components/providers/__tests__/WebSocketProvider.reconnect.test.tsx src/components/providers/__tests__/WebSocketProvider.rehydrate.test.tsx src/hooks/sessionAttachReplayRequired.test.tsx src/lib/websocket.test.ts src/hooks/useChatMessaging.test.tsx
```

结果：退出码 0，6 个文件、47 项全部通过。Provider 26 项，重连 1 项，恢复 1 项，回放 3 项，发送协议 6 项，消息 hook 10 项。

两项新增回归分别验证：

- 同一挂载期间改变活动会话、聊天会话和运行会话集合，以及处理实际形状的 `flow.started` / `flow.finished` 事件，正确发送 attach/detach，保持 `connected=true`；初始化、就绪检查和 connect 均只运行一次，卸载时才 disconnect。
- 离线期间改变目标会话后，同一个连接订阅的重连回调只 attach 最新目标，不捕获旧会话集合，也不因目标变化重建连接。

上述两个文件的 `git diff --check` 通过。后续真实静态页面已复制最新 Provider、重建并复测通过：任务 socket 关闭次数为0，最终完成状态无断连提示，见 [T53-ui](T53-ui.md)。最终完整门禁亦覆盖本修复。

## 最终只读审查边界

同时对照了 Go `task_contract.go` 与最新前端发送、同次发送重试实现：`sendTask` 提供或保留 `request_id` / `idempotency_key`，同时置于顶层与 data；同一次发送失败后的重试复用原标识，符合后端任务去重契约。会话接入继续由 Provider 维护。

对 T31 的直接流程启动、主编排排队启动、人工确认恢复、跨服务重启的检查点恢复和事件水位持久化调用进行了只读核对，未发现额外有证据的实际遗漏。相应既有测试确实断言了步骤回执、权威快照、恢复不重复执行下一步、重启恢复和序号不回退；本轮没有再次执行这些 Go 测试，最终执行结论以根任务门禁和 T53-ui 真实链路为准。此审查不构成真实 LLM 或视觉模型质量验收。
