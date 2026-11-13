# 调研：Dify 会话记忆（与 I6 chat 压缩对读）

> 2026-11-13 · 深度组件冲刺 B 切片 · 对标对象：Dify TokenBufferMemory（Chatflow/Agent
> 多轮上下文管理）。动机：I6 确定性折叠 vs Dify 的记忆策略——验证设计取舍与捞残余借鉴。

## 一、Dify 原生记忆机制

- **TokenBufferMemory**：Chatflow/Agent 应用内置；LLM 节点级配置（memory 开关 +
  window 大小/条数或 token 上限）——**原生只有 buffer/window**。
- Start 节点内置会话历史变量 + LLM 节点 memory 开关 = 离线 Chatflow 的多轮上下文组合。
- **summary / vector 记忆非原生**：需提示工程、外部插件（如 Mongo 记忆插件）或自定义
  workflow 实现。

## 二、已知坑（社区实证）

- **两个 LLM 节点不自动共享记忆**（GitHub #8321）——每个节点独立配置 memory。
- Chatflow 记忆异常报告（issue #12944）。
- API 调用时 `conversation_id` 管理是 agent 记忆生效的关键。

## 三、与 I6 对读

| 维度 | Dify TokenBufferMemory | agentfleet I6 |
|------|----------------------|---------------|
| 策略 | window（N 条/token 截断） | 阈值触发确定性折叠（20/12，40 rune 单行摘要）+ 保留近期全文 |
| 成本 | 零 LLM 成本 | 零 LLM 成本（同为确定性文本折叠） |
| 粒度 | per-LLM-node 配置 | per-conversation（chatHistoryTranscript） |
| 群聊 | 无共享（节点间不共享是坑） | room 全量时间线显式共享（I31 设计） |
| summary/vector | 非原生（插件/自定义） | 未实现（确定性折叠已够个人级） |

**同构确认**：两者都选了「零 LLM 成本的确定性上下文治理」；我们的折叠更进一步
（保留摘要行而非纯截断——丢失的是细节不是事实）。

**显式共享 vs 隐式坑**：Dify 的多节点记忆不共享是用户常踩的坑——我们群聊的共享时间线
是显式设计（数据流即设计意图），方向正确。

## 四、可选演进（不急）

- I6 加 summary 模式（LLM 摘要压缩替代确定性折叠）——触发条件：实测长会话折叠丢关键
  事实时才值得（确定性折叠丢失的是措辞细节；单行摘要按首行截断，事实保全率待测）。
- 分类记忆隔离（按主题分会话）——会话本身已是隔离单元。

## 五、明确不抄

- TokenBufferMemory 的纯截断窗口（折叠保留摘要优于截断）。
- vector 记忆（个人级会话规模不需要；且引入 embedding 管道）。

## 六、来源

- [vilix.ai：Dify 会话记忆](https://vilix.ai/blog/dify-remembers-the-chat-your-scheduled-workflows-still-start-from-zero)
- [GitHub #8321：多 LLM 节点记忆不共享](https://github.com/langgenius/dify/discussions/8321)
- [LLM 应用记忆策略对比（apxml）](https://apxml.com/courses/prompt-engineering-llm-application-development/chapter-5-building-applications-llm-frameworks/managing-memory-llm-applications)
- [多轮上下文压缩五技巧（CSDN）](https://blog.csdn.net/VarPerch/article/details/155674244)
