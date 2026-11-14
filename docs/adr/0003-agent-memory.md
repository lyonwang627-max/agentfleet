# ADR-0003: Agent 记忆架构设计（三层记忆与写入管控）

> 状态：已采纳（2026-11-14）｜背景：I6 chat 压缩（确定性折叠）与 Skills 经验沉淀落地后，
> 记忆面缺「跨会话持久层」——Dify 变量系统调研确认 conversation.* 是唯一缺位，
> LangGraph Store / Mem0 / ChatGPT Memory 是长期记忆的业界参照。
> 价值：本 ADR 把分散的记忆能力（时间线/Skills/折叠）统一成分层模型，并定「写入管控」与
> 「确定性注入」两条铁律——与既有设计原则（指令通道纯净、平台侧审批）同源。

## 一、三层记忆模型

| 层 | 内容 | 生命周期 | agentfleet 载体 |
|----|------|---------|----------------|
| **工作记忆** | 当前 run 的上下文窗口（prompt/观察/交接历史） | 单 run | prompt + run_steps（I6 折叠治理膨胀） |
| **会话记忆** | 会话内跨轮状态与消息时间线 | 会话期间 | messages 时间线（I31 群聊共享时间线）+ **M3：会话持久变量 KV（缺位补齐）** |
| **长期记忆** | 跨会话/跨 agent 的陈述性知识（事实/偏好/决策） | 永久（可失效） | **M1/M2：memories 表（缺位补齐）** |

**与 Skills 的边界（关键区分）**：Skills = 操作性 SOP（**怎么做**——修 P99 告警的步骤），
memories = 陈述性事实（**知道什么**——staging 库是只读副本、用户偏好 pgx 而非 gorm）。
两表并存不混：Skills 注入走「参考资料」通道，memories 注入走「已知背景」通道。

## 二、长期记忆设计（M1/M2）

### 数据模型（M1）

```
memories 表：
  id, agent_id（agent|squad|global 三级 scope 由 agent_id 语义扩展）
  scope      TEXT    -- agent | global（M1 只做 agent 级；global=跨 agent 共享为 M3）
  kind       TEXT    -- fact | preference | decision
  content    TEXT    -- 一句话陈述（确定性文本，无 embedding）
  source_run_id INT   -- 抽取来源（可追溯——对齐 F17 处方的 source 语义）
  status     TEXT    -- draft | active | expired
  created_at, approved_at
```

### 写入路径（铁律：不静默写入）

1. **抽取**：run 完成后 judge 旁路抽取「值得记住的事实」（LLM 一次调用，产出草稿）——
   草稿进 draft 池，**不生效**。
2. **审批**：人审 API（approve → active / reject → 丢弃）——对齐 I29 审批门与 F17 处方的
   人审闭环；**原则：他人发言不能替操作人授权 → 模型产出的记忆不能未经人审自动生效**。
3. **手动直写**：`POST /agents/{id}/memories`（API 直写 active）——运维/用户显式注入。

### 读取路径（铁律：确定性注入）

- execute 时按 agent_id 取 `status=active` 的记忆 → 注入 system prompt 的
  「已知背景（长期记忆）」段——**确定性注入，不跑 LLM**（对齐 I6/I21 原则）。
- **预算上限**：条数（≤10）+ 字符（≤2000），超限按 created_at 最近的优先——防记忆
  膨胀反噬上下文（I6 同款教训）。
- 失效：人工 expire（事实过期如“部署环境已迁移”）——不做自动衰减（可审计优先）。

## 三、会话持久变量（M3，conversation.* 等价）

- conversations 表加 KV：`POST /conversations/{id}/vars {key, value}`（Variable Assigner
  等价的 API 形态，无 workflow 图节点）。
- I6 折叠时注入「已知状态」段（KV 内容入 transcript）——群聊 @all 智能选人、试聊多轮
  直接受益。
- 触发条件：实测时间线折叠丢关键状态才值得（当前全量时间线+折叠已够）。

## 四、检索策略：个人规模不引入向量

- mem0/LangGraph Store 的向量检索解决「记忆多到无法全量注入」——agentfleet 个人规模
  （单 agent 记忆 ≤ 数十条）**全量注入+预算截断即可**，embedding 管道是过度设计。
- 升级触发条件：单 agent active 记忆 > 50 条且注入预算频繁截断——届时再引入向量
  （pgvector 已有基建，pg 零新依赖）。

## 五、明确不抄

- **自动写入不人审**（Mem0 默认自动记忆）——违背管控面定位；模型产出不能静默变成
  agent 的「信念」。
- vector 检索默认化（见上，个人规模过度设计）。
- ChatGPT 式隐式记忆（无审计追踪）。

## 六、落地排期

- **M1**（S）：memories 表 + 手动 CRUD API + execute 确定性注入（预算上限）
- **M2**（S）：run 完成抽取草稿池 + approve 闭环（F17 处方模式复用）
- **M3**（S）：会话持久变量 KV
- 可选：向量检索（触发条件见上）、global scope、memory 事件流出站（I7 webhook 订阅
  memory.approved）

## 七、面试讲点

1. **三层模型与业界对齐**：工作/会话/长期三层 = LangGraph（短期 memory + 长期 Store）、
   Dify（window + conversation.*）的通用分法——我们补齐缺位层时保持了架构一致性。
2. **两条铁律同源**：写入不静默（人审闭环）+ 注入确定性（不跑 LLM）——与「指令通道
   纯净」「平台侧审批」一脉相承，不是新发明而是原则复用。
3. **与 Skills 的边界清晰**：SOP vs 事实、参考通道 vs 背景通道——回答「为什么不合并
   一张表」有据。
4. **反直觉取舍**：不引入向量检索（个人规模全量注入更简）——回答「为什么不用
   embedding」有据。

## 八、来源与关联

- [LangGraph Store（长期记忆参照）](https://langchain-ai.github.io/langgraph/concepts/memory/)
- [Mem0 记忆层](https://mem0.ai)
- [Dify 变量系统调研](../research/dify-variable-system.md)（conversation.* 缺位）
- [Dify 会话记忆调研](../research/dify-conversation-memory.md)（window vs 折叠）
- agentfleet 内部：I6 折叠 / Skills / F17 处方闭环 / I29 审批门
