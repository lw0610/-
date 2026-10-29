# 版面产出声明与执行器契约修复

2026-10-29。隔离模型回放已触发：排版步骤申报 `{"id":"layout_done","kind":"file","format":"layout"}`。原因是 `plan_propose` / `plan_revise` 向模型公开的 `produces.kind` 枚举缺少 `layout`，而计划规范和执行器原本都支持工程版面。该错误声明进入文件校验，导致已经建立的版面仍被要求提供 `.layout` 文件。

修复在声明入口完成：

- 两个公开工具的 `steps.produces.kind` 加入 `layout`；说明其产出值为工程中的版面名称，不填文件格式。`data.kind` 不变。
- `NormalizeV2` 只在明确 `format=layout` 且 `kind=file` 或空时改为 `kind=layout`，并移除格式。不根据步骤标题推测，也不放宽文件核验。
- PDF、QPT 仍为 `kind=file`，保留对应格式和文件存在检查。

回归 `TestV2LayoutProductNormalizesThroughExecutionVerification` 使用实际错误声明，经过 `Decode → NormalizeV2 → ValidateV2 → planexec.Build → VerifyOutputs`，确认版面名交给 `layout_info` 读回；同一步 PDF/QPT 仍走文件核验，删除 QPT 后失败。`TestL30RealToolSchemaReachesModelWithNestedContract` 验证两份真实工具 YAML 投影到模型后有 layout 产出类型，且获取数据类型未改变。

定向命令通过：

```sh
cd carto_agent
go test ./internal/plan ./internal/planexec ./internal/flowrun ./internal/skill \
  -run 'Test(V2LayoutProduct|L30RealToolSchema|V2Normalizes|OutputValidationFiles)' -count=1
```

没有修改该隔离回放的持久化计划、运行状态或成果；修复后的完整模型回放结果由主任务另行记录。
