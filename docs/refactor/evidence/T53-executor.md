# T53 补充：ScriptExecutor 终态发布竞态

完整门禁在 `test_script_executor_uses_main_thread_runner_when_available` 中偶发读到 `status=succeeded`、`result=None`，而原断言要求实际脚本结果为 `ok`。这不是证实 Qt mock 污染：生产写入顺序本身存在可复现竞态。

## 事实与修复

原 `_execute_task` 先调用 `_update_task_status(SUCCEEDED)`，再另取锁写入 `task.result`；`wait_for_task` 看到终态就返回，因此可能正好位于两次写入之间。失败和超时分支同样先公布 FAILED 再补 error；completed_at 到 finally 才写。原 `_update_task_status` 还在释放任务锁后写 status。

只修改以下两个文件：

- `autocarto/tools/system/_shared/pyqgis_executor_impl.py`：在 `_update_task_status` 的同一把锁内，先写终态的 result、error、completed_at，再写终态 status。成功、错误、超时、异常与取消均走这个发布边界；finally 只释放并发槽。`get_task_status` / `list_tasks` 在同一任务锁内生成快照，避免拿到可变引用后再逐字段读取。`wait_for_task` 的轮询与终态判断没有修改。
- `autocarto/tests/test_pyqgis_executor_impl.py`：原测试和断言保留；新增同步屏障，在真正写入终态的边界冻结写线程，核对 result/error/completed_at 已经就绪。覆盖成功、失败、超时三种结果，不依赖调度概率、日志措辞或额外 sleep。

未放宽原断言、测试超时或生产超时；未替换主线程 runner、脚本环境或结果格式。没有修改其他测试来取得本次执行器回归通过。

## 实际验证

- 修复生产代码前，新同步回归稳定得到三个失败：成功缺少 `ok` 结果、失败缺少 `broken` 错误、超时缺少超时错误。日志 `autocarto/tests/golden/.runs/T53-executor-before.log`。
- 修复后，整个目标测试文件通过：4 passed、3 subtests passed，含原主线程 runner 用例、取消进度用例和公共状态用例。日志 `T53-executor-after.log`。
- 首轮普通完整矩阵（排除 `*_qgis.py`）中执行器全部通过，290 passed、509 subtests passed、3 skipped；唯一失败为另一旧测试 `test_session_manager.py:101` 用 `/var` 的字面临时目录与解析后的 `/private/var` 比较。该问题已报告主代理，未扩大本子范围修改。日志 `T53-executor-ordinary-matrix.log`。

- 主代理独立规范化该 session_manager 测试的预期物理路径后，再次完整运行普通矩阵：**291 passed、509 subtests passed、3 skipped，29.10 秒，exit 0**。没有强制 TMPDIR 来隐藏路径别名，也没有修改执行器原断言。最终日志 `autocarto/tests/golden/.runs/T53-executor-ordinary-matrix-final.log`。主代理后续完整 `build.sh check` 另行覆盖总体门禁。

本补充验证使用 QGIS 自带 Python launcher、独立 AUTOCARTO_HOME 和新副本根目录；只运行目标文件及普通矩阵，没有重跑 15 个独立 QGIS 模块。三个 skip 仍是未构建发行产物的原有发行包检查。
