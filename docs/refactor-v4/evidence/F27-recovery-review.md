# F27/F28 恢复与会话文件登记独立复核

已完成以下修复与回归。测试只使用临时目录、合成文件和受控工具返回值，不读取真实用户运行目录，不调用外网或模型。

- JSON 持久化后清单文件映射变成 `map[string]any`；恢复时追加下一条获取结果保留先前所有行，实际输入与资源覆盖保持一致。
- 运行记录保存真实 `project_path` 并在恢复、重启时保留。获取与步骤输出登记到已确定的 `session_dir`，允许会话目录、实际 QGIS 项目目录和获取仓库；应用根目录不能作为项目目录扩大访问范围。相对成果按会话目录解析，旧记录无项目路径仍支持会话成果。
- 手动提供数据沿用 `Engine.ValidateProvided`：错误几何、缺少必需字段和不可读文件不能推动流程；更新失败原因并继续等待确认，换成有效数据后恢复成功。原文件保持不变，验证副本清理。
- 多个系统获取步骤使用互异确认运行 ID，已完成确认的重复返回被忽略，不会误恢复下一条获取项。
- 运行记录写入失败时预取工作停止、会话与等待锁释放，状态保留中断。

验证命令与结果：

```text
go test ./internal/flowrun/... ./internal/acquire/... ./internal/sessionfiles/... -count=1
flowrun 4.028s / acquire 0.953s / sessionfiles 1.790s：全部通过

go test -race ./internal/flowrun -run 'TestAcquisition(RestoredList|ProvidedInvalid|StepsHaveDistinct|StorageFailure)|TestFlowFilesUseActual|TestProjectPathSurvives|TestLegacyRunWithoutProjectPath' -count=1
2.504s：通过，无数据竞争
```

新增七组回归在 `internal/flowrun/acquisition_recovery_review_test.go`。`ProjectPath` 贯通和手动数据核验实现随 F27/F28 主提交保存；确认 ID 与失败取消修复由主执行代理提交。文件登记与恢复映射在独立 `acquisition_outputs.go`。
