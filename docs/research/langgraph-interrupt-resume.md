# 调研：LangGraph interrupt/resume（human-in-the-loop 机制）

> 2026-11-10 · 深度组件冲刺 B 切片 · 对标对象：LangGraph 1.x（agentchat 之外最主流的
> HITL 设计）；与既有 langgraph-checkpointer.md 互补（那篇讲恢复基建，这篇讲恢复语义）。
> 动机：agentfleet 审批门（review→approve→requeue）与 Steer 插话已有任务级 HITL，
> LangGraph 的动态 interrupt 是「执行中任意点暂停并恢复」的成熟参照——直接细化 I11。

## 一、核心机制

**interrupt(payload)**：node 内任意点调用——runtime 抛可恢复异常，图暂停、
checkpointer 写入精确状态（含"中断中"），payload 经事件流暴露给调用方。
**恢复契约**：`Command(resume=value)` 传入的值成为 node 内 `interrupt()` 的返回值。
生产必须持久化 checkpointer（内存版只适合演示）；`thread_id` 是状态的持久游标。

**关键语义**：resume 时 **node 从头重新执行**——interrupt 之前的代码会重放。
由此推出四条纪律：
1. interrupt 前的副作用必须幂等（upsert 而非 create）；
2. 多次 interrupt 严格按索引匹配 resume 值列表——不能条件性跳过或循环调用；
3. 不要裸 try/except 包裹 interrupt（会吞掉中断异常）；
4. 不要在 node 内 while 循环 interrupt（重放会迭代全部历史）——错误提示存 state、
   条件边回环。

## 二、三种 HITL 模式

| 模式 | 语义 | resume 值 |
|------|------|----------|
| Approve/Reject | 关键操作前暂停 | 布尔 + `Command(goto=...)` 条件路由 |
| Review & Edit | 人审阅并修改 LLM 输出 | 修改后的内容直接写回 state |
| **工具内 interrupt** | 审批逻辑随 `@tool` 走 | 可覆盖工具参数（approve 时改 subject 后再执行） |

工具内 interrupt 是粒度最细的形态：高风险动作的审批定义在工具本身，
而不是包在任务外层。

## 三、对 agentfleet 的借鉴

1. **粒度分层已成立，粒度下沉是缺口 → I33 入池**：我们的审批门是任务级
   （review→approve→requeue 整任务重跑），LangGraph 证明审批可以下沉到工具调用粒度
   ——risk 分级（I29）下沉：high 任务的特定工具调用前暂停，approve 带参数修正后执行。
   这是「approve 带意见注入 prompt」（F3）的自然进化。
2. **node 重放 + 副作用幂等 = I11 的第三块拼图**：此前 Temporal 调研给了「结果重放」语义、
   LangGraph checkpointer 给了「super-step 落库」，本篇补上执行层约束——**恢复=重放，
   所以确定性步的工具观察必须可重放、副作用必须幂等**。I11 验收标准再+1 条：
   重试续跑时 tool 步返回已记录观察（不重复调 MCP）且副作用幂等（与 Temporal 结论同构，
   双源互证）。
3. **resume 值契约 ↔ Steer 同构**：interrupt() 返回 resume 值——我们 Steer 插话在
   决策边界注入 user prompt，本质是同一契约的文本版；I11 落地时 resume 值应结构化
   （而非纯文本）。
4. **多 interrupt 按 ID 映射恢复**：并行分支各自中断时 resume_map = {interrupt_id: value}
   ——我们 handoff 链上多个 review 门若并存，需要类似的 (run_id, gate_id) 寻址。
   当前单任务单门足够，记为演进注意项。

## 四、明确不抄

- **动态图框架本身**（条件边/子图/状态机编排）：我们任务是平铺 run 步骤 + handoff，
  不需要图运行时（同 checkpointer 调研结论）。
- **thread_id 会话持久化**：任务状态已在 tasks/runs 表，DB 即 checkpointer。
- **response_schema 校验 resume 值**：单用户平台，审批意见为自由文本。

## 五、来源

- [Interrupts（官方文档）](https://docs.langchain.com/oss/python/langgraph/interrupts)
- [interrupt API 参考](https://reference.langchain.com/python/langgraph/types/interrupt)
- [Interrupts and Commands 实战](https://dev.to/jamesbmour/interrupts-and-commands-in-langgraph-building-human-in-the-loop-workflows-4ngl)
- [Deep Agents HITL（checkpointer 依赖强调）](https://docs.langchain.com/oss/python/deepagents/human-in-the-loop)
