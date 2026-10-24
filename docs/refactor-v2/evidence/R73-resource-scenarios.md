# R73 资源专项验收补充

本记录覆盖 `07` R73 第 3、4 项，以及旧式随包资源修复入口；完整门禁、五项离线运行和性能/守卫由主 R73 记录汇总。

## 验证实现

新增 `carto_agent/internal/api/resource_v2_acceptance_test.go`，通过实际 `API.SetupRoutes`、正式资产目录、当前默认智能体方案、流程存储、资源 Resolver 和编译/链接链路验证；模型设为空，使用确定性回退，不访问模型网络服务。公共资源与内置流程只读，导入包、用户覆盖、配置、绑定和数据存在性夹具全部置于测试临时目录。

### 行军流程可移植与绑定稳定

`TestResourceV2MarchingPortablePackageAndStableBinding`：

1. 实际 `autocarto/flows/system/marching-route` 通过 HTTP 导出，显式附带公共依赖。
2. 创建完全空的公共资源库与运行目录，通过 HTTP 导入并体检。两个公共依赖成为 `bundled`，三个场景资源仍为 `flow`；公共资源库列表始终为空，没有静默安装资源。
3. 除外部数据清单外，所有资源诊断均没有错误。按照原始 CSV 的八条路径在临时目录生成存在性夹具及 `.shx/.dbf/.prj`，通过正式绑定接口选择该目录。
4. 绑定后完整体检计划无 error；通过 HTTP 保存将 `「中国底图」` 的标签及正文引用改成 `「迁移后底图」`，文件位置不变。
5. 再次体检无 error，资源键仍为 `path:data/中国底图.csv`，绑定键、基准目录、八个已存在条目均保持不变。

此测试验证解析/链接与绑定，不将存在性占位文件当作可用于真实 GIS 运算的数据。真实 GIS 工具契约由 R71、完整离线运行由 R73 主记录覆盖。

### 四角版式显式覆盖和恢复

`TestResourceV2FourCornerOverrideProtectedDeleteAndRestore`：

1. 通过实际资源 API 复制 `layout.four-corner`，指定 `override=true`；得到新用户编号并保留明确的 `overrides`。
2. 体检内置 `traffic-route`，版式使用该用户资源并包含 `FLOW_RESOURCE_OVERRIDDEN` 提示，计划无 error。
3. 引用 API 返回受影响内置流程，来源为 `via: lock`。普通 DELETE 返回 409 和受影响流程，资源仍可读取。
4. `force=true` 删除后重新体检，版式回到 `layout.four-corner/library_builtin`，覆盖提示消失，计划无 error。
5. 整个过程内置流程的 `flow.lock` 字节保持原样。

### 旧式随包清单入口

新增 `resource/bundle_test.go` 的 `TestLegacyBundledManifestOffersApplicableRepair`。随包旧资源缺清单时提示安装为公共资源 (`install_resource`)，不再展示只适用于可编辑流程本地资源的 `upgrade_resource` 按钮。

## 命令与结果

显式设置 `AUTOCARTO_PROJECT_ROOT`、`AUTOCARTO_ASSETS_ROOT` 为 enio2，`AUTOCARTO_HOME` 为 `/tmp/enio2-r73.*`。测试内部再以 TempDir 隔离全部状态。

- `go test ./internal/resource ./internal/api -run 'TestLegacyBundledManifestOffersApplicableRepair|TestResourceV2' -count=1`：通过；日志 `/tmp/enio2-R73-resource-scenarios.log`。
- 最终文档/序列化结构冻结后，重建 6 份内置/验收 `flow.lock`、3 份编译计划夹具和 `gb-basemap` 句柄夹具；随后以不设置更新环境变量的专项测试作只读核验。
- `go test ./internal/flow ./internal/resource ./internal/api -run 'TestBuiltinFlowLocksUpToDate|TestCompilerThreeExamplesMatchFixtures|TestResourceHandleFixtures|TestLegacyBundledManifestOffersApplicableRepair|TestResourceV2' -count=1`：通过；日志 `/tmp/enio2-final-resource-verification.log`。
