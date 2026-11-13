# 调研：Dify 变量系统（与 I21 模板变量对读）

> 2026-11-14 · 深度组件冲刺 B 切片 · 对标对象：Dify 变量系统（Workflow/Chatflow 四级
> 作用域）。动机：I21 模板变量（task.title/task.id/workspace.*/agent.name 六变量）是
> 只读注入——Dify 的变量作用域分级（尤其会话级持久变量）是变量面的完整参照。

## 一、四级作用域

| 作用域 | 生命周期 | 写入方 | agentfleet 对应 |
|--------|---------|--------|----------------|
| 系统 sys.* | 单次运行（query/files/conversation_id/dialogue_count） | 平台内置 | Prompt 模板六变量 ✓ |
| 环境 env.* | 全应用持久（API key/资源名），**运行时只读** | 部署配置 | FLEET_* env ✓（gateway 档位/限流） |
| 会话 conversation.* | **跨轮持久**（Chatflow 专用，Variable Assigner 写入） | 节点 | **缺位**——steer_note 是单次插话非持久变量 |
| 节点输出 node.* | 单次运行（LLM/Code/HTTP 节点输出供下游引用） | 节点 | artifacts + run_steps ✓（引用走 trace） |

## 二、已知坑

- Agent 节点的 tool 参数不能直接引用 env 变量——须先映射为 Dify 变量（issue #26949，
  2025-10）——**作用域跨界需要显式桥接**，与我们的「注入只发生在执行副本」原则一致。

## 三、对 agentfleet 的对读

1. **四级中三级已覆盖**（系统模板变量/环境 env/节点输出=artifacts+trace）——唯一缺位
   是**会话级持久变量**：群聊/试聊的跨轮状态目前靠消息时间线全量携带（I6 折叠）。
2. I34 CC session（外部会话记忆）+ I6（折叠）已覆盖会话记忆——conversation.* 的价值
   在**结构化跨轮状态**（如累计确认的关键信息），非自由文本。个人级暂无强需求。
3. env.* 运行时只读原则 ✓（我们 env 只在启动读取）。
4. Agent 节点桥接坑的反面印证：**工具参数与变量作用域的边界必须显式**——I33 工具级
   审批的参数校验路径已覆盖。

## 四、可选演进（不急）

- 会话持久变量（conversation.* 等价）：群聊/试聊的结构化跨轮状态——触发条件为实测
  时间线折叠丢关键状态。形式建议：conversations 表加 KV 列 + 变量分配 API。
- 明确不抄：Variable Assigner 节点图形态（无 workflow 图）。

## 五、来源

- [Key Concepts：变量（官方）](https://docs.dify.ai/en/getting-started/readme/variable)
- [Variable Assigner / Conversation Variables](https://docs.dify.ai/en/cloud/use-dify/nodes/variable-assigner)
- [GitHub #26949：agent 节点 env 变量限制](https://github.com/langgenius/dify/issues/26949)
