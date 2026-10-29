# 候选字段归一化缓存失效与原件复用

日期：2026-10-29。本次为确定性本地回归，没有模型调用、联网取数、用户缓存删除或历史完成台账修改。

## 真实触发依据

隔离地铁回放的旧缓存位于 `/tmp/autocarto-v7-d51-metro3-unrslx9l/data/acquired/source.osm.overpass/aa3bf16a9be89df7`。其 manifest 的 `spec.query` 声明候选字段 `name: [name:zh, name]`；240 条原始站点中，`name:zh` 有 93 个非空值，后备 `name` 有 239 个非空值。旧归一化结果只保留 93 个名称。

Python 已修正为逐要素读取首个非空候选值。然而，旧 processed 缓存仍可能直接返回：正常缓存键原来没有处理语义版本，语义候选查找也没有检查该版本。仅修改归一化函数不会自动纠正旧缓存结果。

## 修改边界

- `cacheSpecification` 仅在来源字段存在多个候选时，将 `revision: field-candidates-v2` 写入既有 `Normalize` 对象。其完整缓存键随之改变。
- `Store.FindSemantic` 从旧 manifest 已保存的 `Query` 读取同一字段候选声明；受影响且没有当前 revision 的 processed 结果不再作为候选。单候选字段的缓存继续复用。
- `baseKey` 仍清除整个 `Normalize`，所以新旧处理版本的原件身份保持相同。原件通过既有 `FindContaining` 复用，然后重新归一化。
- `AdapterVersion`、`SourceVersion`、原始下载缓存键均未变化。旧记录和旧输出仍可按其原键读取，供历史来源记录使用；没有删除、覆盖或篡改旧成果。

## 回归证据

新增 `internal/acquire/normalization_cache_test.go`：

1. 使用真实原始站点 fixture `autocarto/tests/fixtures/metro-stations-osm.geojson`，在临时数据仓构造旧处理 manifest，记录旧结果只有 93 个非空名称。
2. 旧 processed 结果不能进入语义候选，正常处理键变化，但原件 baseKey 不变。
3. 通过生产 `Engine.RunItem` 路径重建，读取缓存原件，下载请求数为零。
4. 新结果进入数据仓，成为唯一可复用的语义候选；第二次执行返回 reused，仅执行既有消费范围派生，没有重新构建缓存结果。
5. 旧输出字节与旧记录均保留；单候选字段处理键和语义复用不受影响。

该 Go 测试的 QGIS 调用边界使用测试替身，用于检验真实 Engine/Store 缓存控制流；不将其声称为真实 QGIS 归一化。实际字段恢复另外通过同一原始 fixture 的真实 QGIS 测试确认：240 条要素中 239 个有效站名，包含医学院、博学路；无候选值的条目仍为空，原始文件字节保持不变。

```text
go test ./internal/acquire ./internal/dataservice -count=1
ok github.com/autocarto/carto_agent/internal/acquire 0.884s
ok github.com/autocarto/carto_agent/internal/dataservice 4.688s

go test -race ./internal/acquire -run 'TestFieldFallbackRevision|TestSingleFieldCaches' -count=1
ok github.com/autocarto/carto_agent/internal/acquire 1.723s

QGIS-LTR Python pytest test_data_acquisition_qgis.py
  -k 'nullable_chinese_name or real_metro_raw'
2 passed, 8 deselected, 2 warnings in 0.79s
```

两条 warning 均为既有 `QgsField` 构造器弃用提示，不影响这次归一化结果。本次没有重跑地铁模型制图，也不把本地回归等同于最新完整制图验收。
