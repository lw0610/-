# F25 在线底图加载补齐（Python）

复核发现此前只有无 Key 的 `tiles.layer.json`，没有消费者，不能满足任务卡“在线底图可安全加载”。本次补齐实际 XYZ/WMS 图层创建与清单复用；Go 侧用户确认、绑定实例与私有凭据授权由主执行代理和工具代理接入。

- 新隐藏系统工具 `data_load_tiles` 读取受限图层定义，固定 HTTPS 天地图 t0 主机与 vec/img/ter 三种 WMTS 样式，不接受自定义 URL 或定义中的 Key。公开参数只有定义路径、确认标记与非敏感认证引用。
- Key 只从 Go 最终桥接派发注入的 `__private_credentials.tianditu` 读取，立即从参数中移除。没有明确布尔确认或没有私有凭据均拒绝。异常使用固定错误，不格式化敏感 URI。
- 当前仓库没有天地图 `tk` 查询参数认证方法。[QGIS 官方认证说明](https://docs.qgis.org/3.40/en/docs/pyqgis_developer_cookbook/authentication.html) 表明 `authcfg` 依赖具体认证方法和 provider，而非任意查询参数替换；安装环境的 APIHeader 是 HTTP Header 认证。没有验证可用方法，因此任意 authcfg 不会绕过确认。实现规范 §5.6 允许的明确确认后保存 Key 到工程地址的回退方式，不声称已实现认证数据库自动保存。
- 输出只返回图层 ID、名称、脱敏 source 和在线标识；相同 URI 复用已加载图层。仅创建图层不证明服务权限或联网渲染可用，返回说明明确保留这一区别。
- 数据清单正式句柄 `layer_id` 复用当前工程中的真实有效图层，不再尝试把定义 JSON 当栅格。图层丢失或类型错误按必需输入失败。
- 全部对外 `layer.source()` 出口、工具结果边界、控制台/全局/会话日志统一屏蔽 Key、token、密码等值，涵盖普通/一次/二次编码 URL、字典参数和异常栈。实际 provider URI 不被修改。

验证采用新临时 AUTOCARTO_HOME、禁用 keyring、QGIS-LTR 自带 Python 和已有 golden/.runtime/deps：

```text
pytest test_tiles_credentials.py test_tool_engine.py test_logger_rotation.py test_data_loading.py
55 passed in 1.42s

pytest test_tiles_qgis.py
1 passed in 0.73s

pytest test_data_loading_qgis.py
11 passed in 2.83s

npm test -- AcquisitionUI.test.tsx model.test.ts && npx tsc --noEmit
17 passed；TypeScript 检查通过
```

真实 QGIS 用例以合成 Key 创建 XYZ 图层，验证真实 provider、重复复用、执行 manifest→tool engine→handler 链路、隐藏目录、source 脱敏、清单按图层 ID 复用与丢失失败。不请求渲染，不使用真实用户密钥或工程；未执行真实天地图在线瓦片验证。

前端显示“等待加载确认”；`flow.acquire_confirmation` 保留该状态，不因缺少 payload.status 被误改成普通等待。输出类型支持无密钥图层定义与已加载图层 ID。

运行注意：每个使用 `isolated_qgis` 的测试模块单独启动进程。最初将两个模块放在同一进程，第二次初始化 QgsApplication 发生原生段错误；分进程后均通过。

## 运行控制器与正式句柄

获取定义后条目进入 waiting_user，第一次加载必须经人工选项明确同意密钥进入 QGIS 工程；拒绝/终止不调用加载工具。确认后仅发送定义路径和布尔确认，凭据由最终绑定实例的私有桥注入。获取文件、缓存与运行 JSON 没有密钥。加载结果的 layer_id 成为单图层有效输入；清单行通过正式 ResourceEntry.layer_id 复用当前工程图层，不把 JSON 定义误当栅格文件。

新增 Go 测试覆盖确认前加载次数为零、拒绝后仍为零、同意后恰好调用一次，以及正式清单句柄保留图层编号。运行恢复保留 layer_overrides；层编号与文件路径分离。当前数据源仍保留 experimental 状态，不把测试密钥/离线建层验证说成用户账户授权或在线可用性验证。
