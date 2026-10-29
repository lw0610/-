# 已加载图层回执的入口规范化

2026-10-29，隔离会话 `07b6c575-15a1-4d7c-902d-4cf13aa2fcb1`，计划 `cp-20261029-184845-28f5ce549406`，运行 `fr-20261029-184850-2d3c071e695b`。

## 实际问题

`route-dataset-contract-fixed-v7-20261029-184741/events.jsonl` 第 207 行首次失败为缺少 `basemap_loaded`。持久化回执其实返回了 `outputs.layers`，其中有八个真实 `{layer_id,layer_name}` 对象，与该次 `get_layers_info` 一致。

第 280 行第二次失败为 `basemap_loaded：图层未找到：map[...]`。回执使用 `basemap_loaded:{省域:ID,地区界线:ID,...}`；旧规范化将整个名称映射视为单个图层描述对象，核验因此找不到 `layer_id/id/name/path/source` 字段。`map[...]` 是错误展示结果，并不是 QGIS 返回的图层 ID。后续失败清理移除了实际新加载的图层，导致模型误以为加载没有生效。

## 修复

- 规范化统一在 `VerifyOutputs` 入口执行，Controller 删除原先重复调用；内部核验消费已规范的列表。
- 只有一个未匹配的 layers 声明时，将未被其他产出声明占用的明确 `outputs.layers` / `outputs.layer_ids` 映射到该声明。多个待匹配声明和任意其他输出名称不猜测。
- 单个 ID、图层描述对象、列表和明确包装集合继续使用同一个 `layerOutputValues`。名称到 ID 的字符串映射按名称排序后转换为 ID 列表；已有图层描述对象保留其路径及元数据。
- 去掉规范化的反复 JSON 编码/解码，通过直接类型转换处理等价形态。无效对象不转换成字符串 ID。
- 所有 ID 继续与实际工程快照核验；格式兼容没有放宽不存在图层、重名、字段或几何不符的检查。

## 回归证据

`carto_agent/internal/flowrun/testdata/layer-receipts-live-v7.json` 只复制该运行前两次回执和匹配的真实 `get_layers_info` 的 ID/名称。原会话、计划、工程与运行记录没有被测试修改。

`TestV7ActualLayerReceiptsNormalizeVerifyAndKeepLoadedLayers` 通过真实 Controller 路径验证两种形态均只执行一次、八层保留、无清理调用，且产出进入规范化后的步骤结果；将工程快照中的一个真实图层移除后仍拒绝回执。另验证多个产出声明或任意其他输出键不会自动猜测匹配。

```sh
cd carto_agent
go test ./internal/flowrun -count=1
go test -race ./internal/flowrun \
  -run 'TestV7(ActualLayerReceipts|LayerReceiptDoesNot|VerifyLayer)' -count=1
```

局部真实回执回归与完整模型恢复是不同证据；后续同会话恢复结果由主任务另行记录。
