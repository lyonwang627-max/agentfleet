# 调研笔记：OpenHands 事件流架构（Event Stream as State）

> 2026-11-07 · 自驱迭代 B 类切片 · 参考：[Event System](https://docs.openhands.dev/sdk/arch/events)、[Agent Architecture](https://docs.openhands.dev/sdk/arch/agent)、[OpenHands 论文](https://arxiv.org/abs/2407.16741)、[v1 演进](https://www.all-hands.dev/blog/the-path-to-openhands-v1)、[SDK 论文](https://arxiv.org/abs/2511.03690)

## 机制是什么

OpenHands 的核心设计：**Agent 的 State 就是事件流本身**——一个按时间排序的 `(action, observation)` 对数列。Agent（LLM）发出 Action → Runtime 执行返回 Observation → 追加到 EventStream → 循环至 AgentFinish。LLM 每轮的上下文就是从事件流回放出来的（历史 action/observation 序列化进 prompt）。

演进：v0 用 pub/sub EventStream 总线连接 agent/UI/runtime；v1（Software Agent SDK）重构为**不可变、强类型事件模型**——ActionEvent/ObservationEvent 等事件子类，状态不再独立存储而是**从事件流派生**（event sourcing）。LLM 成本、委派标记等元数据也作为事件属性随流存储。

## 怎么实现（对我们的启示）

1. **单一事实源**：状态 = f(事件流)，不双写。任何时刻可从流重建完整状态——调试时「回放一段事件」即可复现问题。
2. **不可变强类型事件**：v1 从 pub/sub 字典改为类型化事件子类——消费方可安全模式匹配。
3. **成本/元数据随流**：LLM cost 挂在事件属性上，账单与行为同一时间线。

## 对 agentfleet 的可借鉴点

现状对照：我们已有 `events` 表（append-only，全局 seq）+ `runs/steps` 表，但**状态是独立维护的**（tasks.status 与事件流双写）——正是 OpenHands v0 模式。可借鉴点：
1. **I12（升级）：以 events 为事实源重放任务状态**——任务状态变迁事件已全量落库（task.update），可加「状态重放」校验器：从 events 表重放某任务的全部 task.update 事件，断言与 tasks.status 一致（漂移检测器，防双写不一致的真实护栏，成本小）。
2. **步骤事件类型化**：run_steps 的 kind 已是类型枚举（llm/tool/handoff/gate），可再约束 input/output 的 schema per kind——向强类型事件靠拢。

## 明确不抄的部分与理由

- **全面 event sourcing（状态只从流派生）**：我们的查询需求是关系型（kanban 按状态列过滤、priority 排序、SKIP LOCKED 认领），事件派生物化视图的成本远大于双写 + 漂移校验器。抄校验器，不抄架构。
- **SDK 化重构**：单控制面进程，抽象层级不需要。

## 落地建议

进 IDEAS 池为 **I13：任务状态漂移检测器**（S 量级：events 重放 vs tasks 表一致性校验，可挂进 ci.sh 或调试端点）——把「双写」这个架构妥协变成「有护栏的妥协」。
