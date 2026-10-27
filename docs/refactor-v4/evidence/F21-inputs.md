# F21 输入、检查与记忆子任务

结论：输入集成完成，数据需求展开/规划主实现与 F21 总记录由主任务汇总。新增来源对象结构校验，拒绝没有需求的输入、冲突标记、未知字段/清单行与无效文件；必填和组合条件把自动获取/补齐算作已提供。来源文件校验复用资源后缀目录；Shapefile 同时检查必要配套文件。

输入检查和启动共用 ValidateInputsWithAcquisition，调用 acquire.Plan，不联网、不调用模型。动态计划保存在 run.Plan.Acquisition。保留 inputs/check 的 data 问题数组，顶层增加 acquisition_plan；问题附 step、item、acquirable 和区域候选。补齐只暂缓已有可获取计划对应行的文件/配套缺失诊断，不吞掉清单格式等其他错误。恢复入口也重新检查输入与可用性。

记忆保留原 v1 文件的 flows 路径记录，新增 choices 区域；GetValues 合并路径与来源模式，只保存用户的选项，不存下载结果。清空或改回路径会移除模式；旧绑定迁移不会覆盖新的来源选择。没有新增历史格式迁移层。

验证：强制隔离后 `go test ./internal/flow/... ./internal/flowrun/... -count=1 -timeout=60s` 全通过；定向 API 输入/角色测试通过。新增测试覆盖对象校验、来源无效、清单逐行路径、启动/检查同一结果、共享计划不变、可获取错误提示、保留无关资源错误、记忆深拷贝与旧格式不变。模型调用 0 次。

测试隔离纠正：发现既有 flowrun 测试没有 TestMain，shell 继承了旧项目的 AUTOCARTO_HOME。初次运行测试时 Controller.Start 的输入记忆路径可能访问旧运行目录，新启动测试也可能写入 example/数据 的测试来源选择；未读取真实文件确认、也未清理，以免进一步触碰用户数据。已新增 flowrun/main_test.go 强制临时 HOME、当前项目 assets 与禁用钥匙串，并隔离重跑。不能将此前测试描述为已验证完全隔离；此问题交 F50 的整体测试隔离守卫继续覆盖。

文件偏差：新增 input_choice.go 与 acquisition_inputs.go 分开纯来源对象、运行校验；新增测试入口隔离是验证发现所必需。原角色运行证据的隔离表述应结合本记录读取。
