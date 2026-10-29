# 导出 PDF 被错误要求读图模型：原因与职责修复

2026-10-29，隔离回放 `route-confirmed-dataset-v7-20261029-183507`，会话 `8caa5f34-92a2-4c22-8cd9-70b0e1bfe941`。

## 实际证据

- `events.jsonl:51` 的导出步骤写“不做质量审查，从版面导出 PDF”，产出为 `kind=file, format=pdf`，没有声明视觉能力、角色或执行器；第 53 行返回“导出 PDF 需要支持读图的模型”。
- 第 80 行模型进一步明确 `capabilities:["print-layout"]`，第 82 行仍得到相同错误；第 89 行把排版与导出合并后，第 91 行继续报错。最后模型发起配置读图模型的确认。
- 根因是 `InferCapabilities` 用 `strings.Contains` 匹配“审查”，把“不做质量审查”也当成审图需求；`NormalizeV2` 无论是否已有有效能力声明都会追加推断结果。`AssignPlanRole` 因被补入的 `quality-feedback` 执行视觉模型门禁。
- PDF 导出本身没有要求视觉模型。失败来自职责被错误扩充；模型最终关于“系统强制所有 PDF 导出配置读图模型”的解释不准确。

## 修正范围

`03-计划与执行规范.md` §1.2 和 §2 规定：capabilities 省略或不存在时才由系统推断。实现据此调整：

1. 有效的显式能力作为步骤契约，去重并移除不存在的编号，不再从正文追加其他职责；缺省或清理后为空时才调用推断。
2. 明确的 `route_dataset` 产出保留原有路线知识专用执行器规则。
3. 质量审查职责由显式 `quality-feedback` 或缺省能力时明确的 `review_initial` / `review_final` 产出决定，不再通过正文中的“审查”“读图”等词授予。没有新增否定词名单或 PDF 特判。
4. 两个计划工具公开 `review_initial` 和 `review_final` 产出类型，说明它们与导出 PNG/PDF 文件的区别。`data.kind` 不变。
5. 真正的审图步骤继续走既有视觉模型门禁，没有降低模型能力要求。

## 验证

- plan、planexec、flow 全包通过。
- `TestV2DeclaredExportCapabilityDoesNotAcquireVisionRequirement`：真实导出指令分别使用明确 print-layout 和省略能力，两者经 Normalize → planexec.Build → AssignPlanRole 均不要求视觉模型；显式质量能力与两个审图产出分别在无视觉模型时拒绝、有视觉模型时选择正确模型。
- 有效声明保留、缺省/无效声明推断、路线数据集专用执行器和真实工具 YAML 投影均回归通过，相关测试在 `-race` 下通过。
- 此轮没有改写失败回放的记录、计划或会话状态。后续真实回放结果由主任务记录。

```sh
cd carto_agent
go test ./internal/plan ./internal/planexec ./internal/flow -count=1
go test -race ./internal/plan ./internal/planexec ./internal/skill \
  -run 'Test(V2DeclaredExport|V2PreservesDeclared|V2RouteDataset|V2Inference|L30RealToolSchema)' -count=1
```
