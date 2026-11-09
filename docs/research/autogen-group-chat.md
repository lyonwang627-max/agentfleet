# 调研：AutoGen 群聊编排（GroupChat 选人与会话终止机制）

> 2026-11-10 · 深度组件冲刺 B 切片 · 对标对象：Microsoft AutoGen agentchat（v0.4+ stable）
> 动机：agentfleet 已有 I31 群聊（显式 @ 唤醒 + 链式接力）与 I30 小队（leader 协调），
> AutoGen 的 GroupChat 是「多人会话自动编排」的成熟参照——选人（speaker selection）
> 与终止（termination）两大机制直接对应我们 room 模式的下一步演进。

## 一、三种团队形态与取舍

| 形态 | 选人方式 | 适用 | 代价 |
|------|---------|------|------|
| `RoundRobinGroupChat` | 固定轮转 | 流水线顺序任务 | 无路由能力 |
| `SelectorGroupChat` | LLM 按上下文选人 | 按内容路由（规划→搜索→计算） | 每轮多一次模型调用；依赖 agent 的 name/description 质量 |
| `Swarm` | agent 间 handoff | 交接式协作 | — |

运行循环三步（Selector）：①分析上下文（历史 + 各参与者 name/description）选 speaker →
②被选中者响应并 broadcast 给所有参与者 → ③检查 termination，未结束回到 ①。
每参与者每轮只执行一步（一次工具调用或一次回复）。

## 二、Selector 机制细节

**selector_prompt 三个占位符**：
- `{participants}`：候选名单 `["name1", "name2", ...]`
- `{roles}`：每行 `"<name> : <description>"`（description 是选人的关键依据）
- `{history}`：双换行分隔的 `"name : content"` 对话历史

**双钩子**：
- `selector_func(messages) -> str | None`：完全接管选人；返回 None 回退模型默认选择
- `candidate_func(messages) -> List[str]`：只缩小模型选择的候选集（None/空列表抛 ValueError）；
  仅在未设置 selector_func 时生效

**防重复**：`allow_repeated_speaker` 默认 False（不许连续发言，除非唯一可选）。
**文档经验**：若发现自己在 selector prompt 里为每个 agent 写选择条件，
就该上 custom selector；小模型（Phi-4 级）保持 prompt 极简，强模型可写选择规则。

## 三、Termination 体系

| 条件 | 触发 |
|------|------|
| `MaxMessageTermination` | 消息数达上限（防死循环兜底） |
| `TextMentionTermination("TERMINATE")` | 消息含指定文本（agent 自主宣告完成） |
| `TokenUsageTermination` | token 用量达限（预算熔断语义） |
| `TimeoutTermination` | 超时 |
| `ExternalTermination` | 程序外部触发（`.set()`，外部通道叫停） |

**组合**：`a | b`（OR）/ `a & b`（AND）——典型 `TextMention | MaxMessage | Timeout` 三保险。
每次 `run()` 后条件**自动 reset**；结果 `TaskResult.stop_reason` 说明为何停
（如 "Text 'TERMINATE' mentioned"）——终止原因可观测。

## 四、对 agentfleet 的借鉴

1. **候选集收窄（candidate_func 思想）→ I32 入池**：room 的 `@all` 现在全员唤醒（token 浪费
   且常不必要）。可改为：显式 @ 优先（权威通道不变）；@all / 未指定时由 LLM 从 roster 中
   选最相关 1-2 名候选集内选人——我们 I30 花名册已带成员技能描述，selector 所需数据已就位。
2. **Termination 组合 + stop_reason**：room 防爆炸现为硬编码常量（depth≤2、单轮≤4）。
   AutoGen 的组合式条件 + 「终止原因写进结果」值得学——唤醒停止时给用户一行可读原因，
   而非静默截断。
3. **allow_repeated_speaker=False ↔ woken map 去重同构**：我们 I31 的唤醒去重已实现同语义。
4. **ExternalTermination ↔ Steer 同族**：外部通道影响运行中会话——我们已有 Steer（运行中插话）
   与 cancel 传播，语义覆盖。
5. **description 即选人依据**：验证了 I30 花名册注入技能描述的路线——若上 I32，selector 的
   输入数据（name + description）已就绪。

## 五、明确不抄

- **LLM selector 替代用户显式 @**：派发必须可解释可审计（同 eino-adk 调研结论
  「任务级显式、步骤级可自动」——自动选人只放在候选集收窄这个「步骤级」环节）。
- **Swarm/handoff 团队形态**：我们已有 handoff 状态机 + 小队模式（leader 协调制），
  表达力覆盖。
- **组件序列化框架**（dump_component）：单控制面进程不需要。

## 六、来源

- [Selector Group Chat（官方文档）](https://microsoft.github.io/autogen/stable//user-guide/agentchat-user-guide/selector-group-chat.html)
- [Termination 教程（| / & 组合语义）](https://microsoft.github.io/autogen/stable//user-guide/agentchat-user-guide/tutorial/termination.html)
- [conditions API 参考](https://microsoft.github.io/autogen/stable//reference/python/autogen_agentchat.conditions.html)
- [Discussion #5997：selector 结束会话的模式](https://github.com/microsoft/autogen/discussions/5997)
