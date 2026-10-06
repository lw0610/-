---
name: data-loading-inspection-vector
description: 结构化矢量数据加载与质量检查试点流程，固定执行数据加载、图层验证、几何检查和报告输出
kind: workflow
tools:
  - data_load_vector
  - get_layers_info
  - check_validity
  - get_active_layer_info
---

# 矢量数据加载与质量检查（结构化试点）

该 workflow package 仅作为 `workflow/v1` 结构化契约试点使用。

## 固定流程

1. 加载矢量数据
2. 验证图层是否进入当前工程
3. 检查几何有效性
4. 输出图层概况报告

## 约束

- 仅覆盖矢量路径
- 不包含条件分支、回退路径或多智能体调度
- 不定义 `tools/*.yaml`
