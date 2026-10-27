---
name: flow-control
display_name: 流程控制
description: 预览、校验并启动当前方案已启用的流程。
kind: category
runtime: internal
domain: 系统
system: false
hidden: true
auto_mount: main
tools:
  - flow_start
  - flow_preview
---

# 流程控制

依据当前会话中的可用流程摘要选择执行方式。用户明确指定某个流程时，先用预览检查其步骤、负责角色、资源和输入。计划或输入存在错误时，报告具体诊断和需要修复的位置；不要声称已经开始执行，也不要改用多个自由委派来绕过流程约束。

输入按执行计划中声明的编号传递。原文类输入始终使用 `{"$ref":"current_user_message"}`，由运行时读取当前用户消息，不能复述、摘要或混入执行说明。必填输入或资源绑定缺失时，只询问确实缺少的项目；默认值可满足的选项沿用默认值。

流程摘要的 `acquirable` 表示输入声明了数据需求，`acquire_available` 表示本机当前存在可用获取方式，`modes` 列出可选方式。按系统消息的 `acquisition_policy` 执行：`ask` 在用户未表态时只询问一次“由您提供，还是由我自动获取”；用户表示没有数据、让系统找或无所谓时传 `{"$acquire":true}`；`allow` 对缺失且可获取的数据直接选择自动获取；`deny` 禁止 `$acquire` 和 `$fill`，请用户提供。用户已给出数据时优先使用；文件夹缺项且策略允许时用 `{"$fill":true,"path":"用户真实提供的文件夹"}` 补齐。

`acquire_available=false` 时不能要求自动获取或补齐。不得编造路径，也不得传 `source`、`sources` 指定来源编号；来源选择由流程表单负责。预览返回 `acquisition_plan` 和带 `acquirable` 的问题项，根据具体缺失项继续沟通。启动返回 `acquire` 时向用户说明哪些数据将自动获取及其来源；预览与启动登记均不代表数据已下载完成。

调用启动流程后检查返回结果。只有 `accepted=true` 才代表启动请求已被接收。此时结束本轮编排，简短告知已启动对应流程，不再调用任务委派或重复启动。流程控制器负责逐步执行、产出校验、失败选择和最终交付；步骤执行中不要自行启动另一个流程。预览为只读操作，可在规划模式下使用。
