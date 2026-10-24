# V40 子项：本机输入记忆与旧绑定启动迁移

## 结论
本子项完成；V40 的资源关联、即时校验、接口与运行开始集成由主卡统一验收。代码和测试均未读写真实运行目录。

## 改动摘要
- `flow/input_memory.go`：`flow-input-memory/v1` 本机 YAML 存储；按文件路径共享进程内锁；临时文件、fsync、原子替换；仅接受绝对路径，文件权限为 0600；读取不创建目录或文件。
- 对运行输入只记住允许记忆的文件/文件夹；空值和改成“不记住”的输入清除旧值。支持手工设置/清除与删除整个流程的记忆。
- `Store.MigrateInputMemory`：匹配实际 v1 `resource_6d294f11b03f` 算法、v2 `path:`、资源清单 ID；相对值按项目根规范化为绝对路径，允许原绑定中的项目根外 `../…`；不依赖原数据目录仍然存在。
- `bindings.Consume`：锁内检查原绑定值没有变化，先备份原始绑定文件，再持久化输入记忆，最后原子移除已迁移绑定。记忆文件修改前备份已有原文件；不存在时无需备份。中途失败源仍在，重试不会覆盖较新的输入记忆。
- 未关联、输入无效/不记住、多个历史值冲突、未知流程和公共库绑定保留只读兼容；绑定为空时保留空文件。
- `cmd/agent/main_deep.go`：初始化流程存储后启动迁移；错误时保留原文件与备份，不打印记忆字典或路径取值。
- `flow/bindings.go`：v1 旧公共名称映射扫描 `retired` 用户资源，防止 V30 列表收窄后丢失旧键身份。

## 运行的命令与结果
所有命令设置 enio2 的 project/assets 目录和 `/tmp/enio2-v40-memory-tests` 运行目录，`AUTOCARTO_DISABLE_KEYRING=1`。测试内再用 `t.TempDir()` 隔离每组 YAML、FLOW.md 和假数据路径。

| 命令 | 结果 |
|---|---|
| `go test ./internal/flow -run TestInputMemory -count=1` | 通过 |
| `go test -race ./internal/flow ./internal/resource/bindings -run 'Test(InputMemory\|Consume\|BindingsMigrate)' -count=1` | 通过；flow 1.885 秒、bindings 2.085 秒 |
| `go test ./internal/flow ./internal/resource/bindings -run 'Test(InputMemory\|Consume\|BindingsMigrate)' -count=1` | 通过，包含随后添加的“不记住”迁移负例 |
| `git diff --check`（本子项文件） | 通过 |

覆盖：读取无写入、不同流程隔离、返回值不可变、空值清除、类型筛选、不记住、非法相对值、未知版本保护、25 个独立存储实例并发写入、精确原文件备份、v1/v2 迁移、新值优先、幂等、冲突不猜测、写目标后中断重试、源值变化拒绝迁移。

## 跳过的测试及原因
- 完整 API/Controller 回归与构建门禁由 V40 主卡及 V50/V51 合并后执行。

## 与方案不一致之处
- 同一目标输入若匹配多个不同的历史绑定值，保留原绑定，避免根据 map 遍历顺序任意选择数据目录。
- 输入显式“不记住”时不会通过启动迁移绕过该属性。
- 已存在且无需修改的输入记忆不生成多余备份；实际改写两份已有文件的场景均先保存各自原始字节。

## 修改了涉及范围以外的文件
- 无。本子项包含两个新的 `bindings/consume` 文件，归属 V40 的 `internal/resource/bindings/**`。

## 遗留问题与后续任务需要知道的事
- `Remember` 仅在运行校验通过且运行记录成功建立后调用；手动 API 负责类型与存在性检查，存储层负责绝对路径及原子性。
- 已与资源/API 执行者约定 `DefaultInputMemory`、`Get`、`Set`、`DeleteFlow`、`Remember` 接口；不能在持有输入记忆锁时反向调用绑定存储，迁移顺序为绑定锁→记忆锁。
- 输入记忆仅位于本机运行配置目录，没有将输入值加进流程包或导出包。
