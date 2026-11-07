# 调研笔记：eino ADK 的 Supervisor 多 Agent 模式

> 2026-11-07 · 自驱迭代 B 类切片 · 参考：[eino ADK 官方文档](https://www.cloudwego.io/docs/eino/core_modules/eino_adk)、[adk 包](https://pkg.go.dev/github.com/cloudwego/eino/adk)、[eino-examples](https://github.com/cloudwego/eino-examples/blob/main/README.md)

## 机制是什么

eino ADK（设计借鉴 Google ADK，Go 优先）提供三种多 Agent 模式：**ChatModelAgent**（单 agent + 工具）、**Plan-Execute**（先规划后执行）、**Supervisor**（协调者模式）。Supervisor 的运作：一个 supervisor agent（LLM 驱动）作为编排者，注册多个子 agent（各带职责描述）；supervisor 根据请求与描述决定委派给谁，子 agent 执行后把结果交回，supervisor 循环协调直到完成。内置工具调用、上下文管理、**interrupt/resume（人审）**流程。

## 怎么实现

supervisor 子包封装了「委派决策 + 子 agent 注册表 + 结果回流」的循环；子 agent 本身也是 ChatModelAgent（可递归嵌套）；人审通过 interrupt 机制挂起/恢复。与 fleetd 的对照：我们的 handoff 是**显式指令驱动**（任务文本里的 handoff: 指令），ADK supervisor 是 **LLM 决策驱动**（模型读描述自动路由）。

## 对 agentfleet 的可借鉴点

1. **委派决策的两种范式互证**：fleetd 选了显式指令（确定性/可审计），ADK 选 LLM 自动路由（灵活/可扩展）——面试可讲两种范式的 trade-off 与各自的适用边界（管控面 vs 编排层）。
2. **interrupt/resume 与审批门同构**：ADK 的 human-in-the-loop interrupt 与我们的 review 状态机语义一致——互相印证设计正确性。
3. **子 agent 描述驱动路由**：ADK 靠 agent 职责描述让 supervisor 决策——我们的 Automations/任务分派可借鉴「描述驱动」改进派发提示（F13-b 两级过滤的 Slow Path 正好用它）。

## 明确不抄的部分与理由

- **LLM 自动路由替代显式 handoff**：管控面的核心价值是可审计可预测，LLM 自动路由放在 runner 内层（任务内的子步骤路由）合适，放在任务级会让派发不可解释。分层：任务级显式、步骤级可自动。
- **直接引入 adk 包重写编排**：F7 已确立「适配器而非重写」的方向；ADK 的 supervisor 循环与我们调度器的差异是领域性的（进程内编排 vs 跨任务管控）。

## 落地建议

进 IDEAS 池为 **I28 步骤级 LLM 路由（ADK supervisor 思想）**（M 量级：F11 多轮循环的 decide 阶段增加「委派给哪个工具/子能力」的描述驱动选择，替代固定 JSON 协议的工具字段——本质是把 supervisor 模式下沉到 run 内部）。与 I19/I18-b 一起排队。
