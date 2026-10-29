# D51 导出工具与 Processing 公开调用边界修复

2026-10-29。本记录是湖南真实回放暴露的问题的本机验证，**没有再次调用模型，也不把湖南未完成的PDF改记为通过**。原始失败见 [D51-hunan-update.md](D51-hunan-update.md)。

## 已确认的共同原因

1. Go把`tool/v1.execution.backend_tool`直接作为远端工具名发送，Python authority registry却以`metadata.name`注册。因此`save_vector_features_to_file → native:savefeatures`到达Python时变成“未知工具”。真实QGIS注册表中`native:savefeatures`存在；缺陷在两层名字的职责混用。
2. `package_layers`的YAML内部算法写成公开名，真实算法是`native:package`。此外Go把`multi_layer`降为string，导致模型发送JSON字符串而不是图层数组。
3. Processing执行器此前成功后只返回字符串`str(result)`，真实文件路径、临时图层对象、计数没有结构化传回Go。因此不能只修两个算法名称。

## 最小修正

- `carto_agent/internal/skill/registry.go`：authority `tool/v1` QGIS工具用公开名称派发；真实算法仍保留在execution manifest，由Python执行器解析。CLI执行目标和旧AtomicSkill路径不改。
- `carto_agent/internal/skill/adapter.go`：`multi_layer`向模型声明为`array`、`items:string`，Go参数映射保持数组。
- `package_layers.yaml`明确绑定`native:package`，可选OUTPUT说明对应当前会话导出目录。`save_vector_features_to_file.yaml`已有正确`native:savefeatures`，不为绕过边界重复改字符串。
- `autocarto/core/tool_engine/executor.py`：按工具声明发布矢量输出和文件路径，保留原生结果字典/计数；内存或文件矢量输出复用现有工程图层发布方法，返回真实layer_id。导出文件返回真实路径、`auto_load:false`；打包结果保留每个子图层URI。没有新增全目录映射、城市特例或通用缓存。
- `join_attributes_by_nearest.yaml`绑定`native:joinbynearest`。此前新加的专用nearest handler只包装同一原生算法和同一图层发布方法，因此删除该handler及两个转发导出，避免保留重复实现。匹配和未匹配数量现在以原生`JOINED_COUNT`、`UNJOINABLE_COUNT`返回，图层引用仍可继续使用。

## 实际验证

| 验证 | 结果 |
|---|---|
| `go test ./internal/skill ./internal/tool -count=1` | 全包通过，1.394秒 / 0.551秒 |
| `TestProcessingPublicDispatchAndNativeManifestRemainSeparate` | 公开名称、原生manifest、array items、数组参数和CLI独立目标断言全部通过 |
| Go Registry→Engine→QGISBackend→Python `execute_tool`→真实QGIS | 4个子测全部通过，总3.283秒 |
| 真实单图层GPKG导出 | 输出文件重新打开，2条要素，郑州市/开封市中文属性保留，路径经Go回传 |
| 真实2图层GPKG打包 | 两个子图层分别重新打开，各2条要素，中文属性正确，子图层URI保留 |
| 不填写OUTPUT的打包 | 自动输出至隔离会话目录，文件及两个子图层重新打开成功 |
| 同一贯通链调用nearest | 真实输出加入工程，有可用layer_id，JOINED_COUNT=2、UNJOINABLE_COUNT=0，计数传回Go |
| `test_join_nearest_qgis.py` | 2项通过：memory和GPKG，真实5单位距离、只复制指定字段、保留未匹配要素、输出引用和计数 |
| `test_vector_selection_qgis.py` | 3项通过：按实际ID选择、真实GPKG保存、双层打包 |
| `test_tool_executor_algorithm_alias.py` + `test_tool_engine.py` | 20项通过 |
| `git diff --check` | 通过 |

新增Go真实贯通测试为`carto_agent/internal/skill/processing_dispatch_test.go`，实际authority manifest由生产tool registry读取YAML生成；Python测试进程`autocarto/tests/fixtures/processing_dispatch_qgis.py`只创建隔离小数据，使用生产QGIS dispatcher，不直接调用handler、不伪造算法结果。通过`AUTOCARTO_QGIS_PYTHON=/Applications/QGIS-LTR.app/Contents/MacOS/python`启用真实边界测试；普通Go回归始终执行目录、schema、dispatch和CLI契约检查。

完整贯通日志：`/tmp/enio2-export-go-qgis-final.log`。所有小数据与QGIS profile位于测试临时目录，测试结束清理。未改用户工程、运行记录、原始配置或缓存；没有消费新的模型调用。

## 验收界限

这证明以上公开工具及成果回传链在本机真实QGIS中已修通。**不代表湖南A4 PDF完整模型验收通过**：该真实回放此前停在402余额不足，另有暂停时QGIS重启导致memory边界丢失的问题。原始失败记录和已产出的GPKG都保留，不修改模型历史。
