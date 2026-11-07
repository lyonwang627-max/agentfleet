# 调研笔记：Temporal 确定性重放（Deterministic Replay 与 Durable Execution）

> 2026-11-07 · 自驱迭代 B 类切片 · 参考：[Temporal Docs](https://docs.temporal.io/workflows)、[Resonate: 确定性约束从何而来](https://journal.resonatehq.io/p/from-where-do-deterministic-constraints)、[Durable Functions 作者谈常见坑](https://blog.cgillum.tech/common-pitfalls-with-durable-execution-frameworks-like-durable-functions-or-temporal-eaf635d4a8bb)

## 机制是什么

Temporal 的 durable execution 建立在**确定性重放**契约上：每个 workflow 动作（Activity 启动/定时器/信号/结果）都作为事件记入持久 history；worker 崩溃或暂停数月后，新 worker 用**同样的代码对着事件 history 重放**——确定性代码产生同样的命令与决策，内存状态（局部变量/调用栈）被完整重建，**无需持久化状态本身**。

关键设计：非确定性操作（API 调用/DB 查询/LLM 调用/随机数/系统时间/线程）全部外化进 **Activity**，其结果作为事件记录；重放时直接返回已记录结果而不重新执行。

## 代码约束（确定性的代价）

Workflow 代码禁止：直接取系统时间/随机数/UUID、直接网络与 DB 调用、原生 goroutine 交错、全局可变状态。违反即重放分叉 → non-determinism error；代码升级需版本化策略（`GetVersion`）。

## 对 agentfleet 的可借鉴点

对照我们的 LLM runner 多轮工具循环（F11-a）：`decide`（LLM 调用）= 非确定性 Activity，`callTool`（MCP）= 外部副作用 Activity，而**循环骨架是确定性的**。借鉴方向：
1. **重放友好的事件语义**（补强 I11）：I11 的「确定性步跳过重放」可以更精确——不是跳过 tool 步，而是**返回该步已记录的观察结果**（Temporal 语义：结果重放而非执行跳过）。I11 的验收标准可据此细化。
2. **确定性约束显式化**：我们的 runToolLoop 已是纯函数（decide/callTool 注入）——天然满足 Temporal 式可测性；可把「decide/callTool 是唯一非确定性出口」写进 runner 包注释作为架构约束。
3. **版本化启示**：F2 的 agent 版本管理 + 处方应用会改变执行语义——若做 I11 步级续跑，跨版本续跑需要版本比对拒绝（对应 Temporal GetVersion）。

## 明确不抄的部分与理由

- **完整 durable execution 框架**：引入 Temporal/Restate 意味着外部重型基础设施 + SDK 学习成本；我们的 SKIP LOCKED 队列 + MemStore/PG 双实现在个人平台规模下收益/成本比更高。抄其「结果重放」语义，不抄框架。

## 落地建议

**细化 I11 验收标准**（入池更新）：失败重试时，确定性步（tool 调用）返回已记录观察而非重新执行；跨 agent 版本（F2 version）变化的续跑请求被拒绝并提示重新评估。量级维持 M。
