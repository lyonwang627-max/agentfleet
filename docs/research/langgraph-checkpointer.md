# 调研笔记：LangGraph Checkpointer（持久化与时间旅行）

> 2026-11-07 · 自驱迭代 B 类切片 · 参考：[Checkpointers 官方文档](https://docs.langchain.com/oss/python/langgraph/checkpointers)、[Persistence](https://docs.langchain.com/oss/python/langgraph/persistence)、[API reference](https://reference.langchain.com/python/langgraph.checkpoint)

## 机制是什么

LangGraph 把图的执行状态（channels/values/pending tasks/metadata）在**每个 super-step（节点执行）之后**序列化存一份 checkpoint。所有 checkpoint 归属一个 `thread_id`（调用时必须传入）；重新以相同 thread_id 调用即从最新 checkpoint 续跑——若最新 checkpoint 无 pending 工作，直接返回已存结果而不重跑节点。可另传 `checkpoint_id` 定位历史任意检查点。

由此解锁四个能力：跨调用的线程记忆、崩溃恢复（重启后带同 thread_id 续跑）、人审暂停（interrupt 后恢复）、**时间旅行**（回退到历史 checkpoint 从该点重放/分叉）。

## 怎么实现

存储后端可插拔（MemorySaver / SQLite / Postgres），核心是 `(thread_id, checkpoint_id)` 二级键 + 状态序列化。社区常见坑：重启后必须传同一 `thread_id`，否则图从零开始，checkpoint 不会自动续上。

## 对 agentfleet 的可借鉴点

我们的 `runs/run_steps` 表已经是「每步落库」的持久层——缺的不是记录密度，而是**从任意步骤恢复执行**的能力：
1. 现在 run 失败只能整任务重试（从零跑）；审批暂停是任务级而非步骤级。
2. 借鉴方向：给 run 增加 `resume_from_step` 语义——重试时若某步骤是确定性的（如已完成的 tool 调用），跳过重放，从失败步骤继续。这与 F11 的上下文 Spill 组合，能做「步级断点续跑」。
3. `thread_id` 的坑对我们天然免疫：我们的 run_id 就是全局键，且 trace 已按 ID 可查（链式召回已实现）。

## 明确不抄的部分与理由

- **时间旅行/分叉重放**：我们的任务是一次性职责单元（不是可 fork 的图实验），整任务重试 + 版本管理已覆盖回退需求；步骤级 fork 会让状态机复杂度爆炸。
- **MemorySaver 内存后端**：我们已有 MemStore/Postgres 双实现的同类分层，无需引入其抽象。

## 落地建议

进 IDEAS 池为 **I11：run 步级断点续跑**（M 量级：失败步骤标记 + 重试时跳过已完成确定性步骤 + trace 标注 resumed_from）。前置依赖：runner 步骤需要「确定性/非确定性」标注（tool 步可跳过，llm 步不可）。
