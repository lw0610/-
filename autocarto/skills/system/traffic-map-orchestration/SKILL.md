---
name: traffic-map-orchestration
description: 交通线路图多智能体编排流程约束。用于用户请求制作、生成、绘制城市或区域交通线路图、道路交通图、轨道交通图、专题线路图时，约束 orchestrator/planner 按 data_engineer -> spatial_analyst -> cartographer -> orchestrator 的阶段顺序交接，并要求每个子智能体输出结构化交接结果。
kind: knowledge
---

# 交通线路图多智能体编排

## 触发条件

当用户请求包含以下意图时，必须启用本流程：

- 制作、生成、绘制、输出某城市或区域的交通线路图
- 制作道路交通图、轨道交通图、公交线路图、专题线路图
- 对交通图层进行整理后再制图导出

示例：`制作一个郑州交通线路图`、`生成郑州市道路交通专题图`、`帮我做一张轨道交通线路布局图`。

## 总体规则

- 不要让单个子智能体吞掉全流程。
- 主智能体只负责任务理解、阶段调度、交接检查和最终汇总。
- 具体工具由对应子智能体根据自己的可用工具和上下文选择。
- 每个阶段结束后必须输出结构化交接结果，再决定是否进入下一阶段。
- 如果任一阶段返回 `status=blocked` 或 `blocking_issues` 非空，回到 orchestrator，由 orchestrator 决定补充上下文、请求用户确认或终止任务。

## 固定阶段

### 1. data_engineer

职责：

- 调用只读项目状态工具确认当前工程、图层、数据文件和 CRS。
- 判断是否已有城市交通相关图层，如道路、轨道、站点、行政区边界。
- 如缺少关键数据，明确报告缺口，不得假装数据存在。

完成条件：

- 产出可供后续使用的图层清单、范围、CRS、数据缺口说明。

### 2. spatial_analyst

职责：

- 基于 data_engineer 的产物执行必要的空间处理。
- 常见处理包括按城市范围裁剪、线路筛选。
- 如果不需要空间分析，应明确返回 `skipped` 并说明原因。

完成条件：

- 产出制图可用的交通线、站点、边界等图层或说明无需分析。

### 3. cartographer

职责：

- 进行区域交通线路图布局制作，按主图四角进行刚性对齐，输出包含主图、标题、图例、数据综述、比例尺、指北针的可交付专题图。

完成条件：

- 产出地图布局名称、导出文件路径、主要图层和制图检查结果。

### 4. orchestrator

职责：

- 检查所有阶段交接结果。
- 汇总用户可见结果，包括已完成操作、输出路径、失败/降级原因和剩余风险。
- 不再启动新的专业子智能体，除非最终检查发现明确缺失项。

## 交接结果格式

每个子智能体结束时必须返回以下 JSON 或等价结构化内容：

```json
{
  "status": "completed | skipped | blocked",
  "agent": "data_engineer | spatial_analyst | cartographer",
  "summary": "本阶段完成了什么",
  "artifacts": {
    "layers": [],
    "files": [],
    "layout": "",
    "extent": "",
    "crs": ""
  },
  "next_agent": "spatial_analyst | cartographer | orchestrator",
  "blocking_issues": []
}
```

`next_agent` 决策规则：

- `data_engineer` 成功后，如存在需要裁剪、筛选、网络整理或几何处理，交给 `spatial_analyst`；否则交给 `cartographer`。
- `spatial_analyst` 成功或跳过后，交给 `cartographer`。
- `cartographer` 成功、跳过或阻塞后，交回 `orchestrator`。
- 任何阶段阻塞时，交回 `orchestrator`。

## 郑州交通线路图默认理解

当用户只说“制作一个郑州交通线路图”且没有提供更多细节时，采用以下默认理解：

- 地理范围：郑州市行政范围。
- 目标产物：交通线路专题图，优先 PDF 导出。
- 数据优先级：当前工程已加载图层 > 工程目录数据文件 > 请求用户补充。
- 交通要素优先级：道路/轨道线路 > 站点 > 行政边界 > 背景底图。
- 需要先检查数据，不得直接进入布局导出。

## 失败处理

- 缺少数据时，停止在 `data_engineer` 阶段，向用户说明缺少哪些图层或文件。
- 工具失败时，每个阶段最多基于错误信息修正重试一次。
- 布局 workflow 失败时，遵守 `cartographer` 中关于 `print-layout-thematic` 的降级规则。
