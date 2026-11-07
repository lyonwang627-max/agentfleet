# 调研笔记：Dify 1.19.0 嵌套工作流追踪（Span 层级保留）

> 2026-11-07 · 自驱迭代 B 类切片 · 参考：[v1.19.0 讨论 #45541](https://github.com/langgenius/dify/discussions/45541)、[Dify Releases](https://github.com/langgenius/dify/releases)

## 机制是什么

1.19.0 之前，工作流经 Workflow Tool 调用另一工作流时，外部可观测平台（Langfuse/LangSmith/OTel 系）只能看到一个不透明的工具 span。1.19.0 起：每次 Workflow Tool 调用成为**父 span**，被嵌套工作流的内部节点（LLM 调用/工具调用/迭代）作为**子 span** 展开——且层级在**嵌套与重复调用**下都正确保持（A 调 B、B 调 C、A 多次调 B 均不乱）。仅对新执行生效，历史 trace 不变。

## 怎么实现

核心是 span 上下文的**跨边界传播**：工具调用边界不再是追踪断点——进入 Workflow Tool 时把当前 span 设为父，嵌套执行内的所有节点 span 挂其下；重复调用靠调用序号/唯一 span id 区分同名层级。

## 对 agentfleet 的可借鉴点

对照我们的 trace：`run_steps` 是**平铺列表**（seq 递增），handoff 链和多轮工具循环的嵌套关系只藏在 note 文本里。借鉴方向：
1. **层级化 steps**（与 I15 同源递进）：给 run_step 加 `parent_seq` 字段——handoff 后新 agent 的 steps 挂在交接步之下；多轮循环的观察步骤挂在其触发的 decide 步之下。瀑布图可缩进展示，嵌套关系从文本推断升级为结构化数据。
2. **跨 run 父子**：handoff 改派产生新 run——新 run 的首步携带 `parent_run_id`，Trace 视图可「跟随交接」跳转，形成 run 树而非孤岛列表。
3. **同名重复调用区分**：多轮循环中同名 tool 步（如连续两次 text_search）靠 seq 天然区分，无需额外处理——我们比 Dify 简单的地方。

## 明确不抄的部分与理由

- **只对 新执行生效的迁移策略**：Dify 面向 SaaS 大规模历史数据；我们的量级可以全量迁移（加字段默认值即可），不需要新旧分治。
- **外部平台集成优先**：Langfuse/LangSmith 对接是它们的商业模式；我们自有 trace 视图，先做好内部层级，OTel 导出（F12）时层级自然映射为 span parent。

## 落地建议

进 IDEAS 池为 **I17 trace 层级化**（M 量级：run_steps.parent_seq + 新 run 的 parent_run_id + 瀑布图缩进渲染 + 跨 run 跳转）——与 I15（eino 回调）、F12（OTel）共用同一层级数据基础，是观测维度从「列表」到「树」的升级。
