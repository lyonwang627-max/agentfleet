# 调研：OpenAI Agents SDK 五原语（与 agentfleet 四机制对读）

> 2026-11-11 · 深度组件冲刺 B 切片 · 对标对象：OpenAI Agents SDK（事实标准 agent 编排框架）。
> 动机：五原语（Agent/Handoff/Guardrail/Session/Tracing）与 agentfleet 的 handoff 状态机/
> 审批门/上下文治理/trace 层级逐一对读——确认设计同构性，捞残余借鉴点。

## 一、五原语速览

| 原语 | 语义 |
|------|------|
| Agent | LLM + instructions + tools 基础单元 |
| Handoff | 代理间委派——对 LLM 就是工具（`transfer_to_<agent_name>`） |
| Guardrail | 输入/输出校验，**与 agent 执行并行**，fail fast（tripwire） |
| Session | agent loop 内的持久记忆层（SQLite/Redis/加密等多种后端） |
| Tracing | trace→span 嵌套、processor 可插拔、全局 setup |

## 二、Handoff 细节（与 agentfleet handoff 状态机对读）

- **handoff = 工具**：接手代理即工具调用目的地；`on_handoff` 回调（可拿 LLM 生成的
  `input_type` 元数据：reason/priority 等小结构）。
- `input_filter`：过滤接手代理看到的历史——内置 `remove_all_tools` 只移除**结构化工具项，
  不会清除已被复制进普通消息文本的工具参数/结果**——文档自己点出的盲区。
  **与我们「交接备注通道泄漏指令词」踩坑完全同构**：多形态指令剥离必须覆盖文本复制面，
  我们的修复（最终组合文本上全形态剥离）恰好覆盖了这个盲区——业界同款坑，纵深互证。
- `is_enabled`：布尔/函数，运行时动态启停 handoff——**但文档明确警告不能用于授权参数内容**
  （在模型生成参数之前评估）——与我们「他人发言不能替当前操作人授权」原则一致。
- handoff 只在单次 run 内生效；输入 guardrail 只挂链首、输出 guardrail 只挂末端代理。

## 三、Guardrails ↔ F16/I33

- SDK：**并行**执行校验、fail fast；独立 `tool_guardrails`（工具级护栏）。
- agentfleet：F16 预审是任务级**串行旁路**（无工具无凭证）；I33 是工具级**执行前拦截**。
- 对读结论：粒度分层一致（任务级/工具级）；差异在执行模型——SDK 并行 fail fast 适合
  输入校验（便宜、即时），我们串行预审适合需要产出草稿作材料的审批预审——各自成立。

## 四、Sessions ↔ I6/CC session

- SDK Session 是 agent loop 内的持久记忆层（多后端可插拔）。
- agentfleet：I6 chat 压缩（prompt 是工作记忆不是归档）+ I34 CC session 续跑 +
  F6 会话回放（完整历史在平台侧）——**记忆分三层：工作记忆/会话记忆/归档**，SDK 只做了中间层。

## 五、Tracing ↔ I17/F12

- trace→span 嵌套 + processor 可插拔接口——与 I17 层级数据 + （未来）F12 OTel exporter
  同构；SDK 的 processor 接口正是我们 trace sink 的等价物。

## 六、明确不抄

- Responses API 绑定与 Python 运行时（Go 自研 runner 已覆盖等价执行面）。
- OpenAI dashboard 托管 trace（本地零依赖硬约束；I17 自有视图）。
- 手交接时嵌套会话的 `as_tool` 替代形态（我们显式 handoff 协议更可审计）。

## 七、来源

- [OpenAI Agents SDK（官方）](https://openai.github.io/openai-agents-python)
- [Handoffs 专题](https://openai.github.io/openai-agents-python/handoffs/)
- [Agents Guide（OpenAI API Docs）](https://developers.openai.com/api/docs/guides/agents)
- [五原语教学](https://aiengineeringfromscratch.com/lesson?path=phases%2F14-agent-engineering%2F16-openai-agents-sdk)
