# 调研：MCP 2025 规范演进（自研 stdio 客户端的升级依据）

> 2026-11-14 · 深度组件冲刺 B 切片 · 对标对象：MCP 规范演进（2025-06-18 → 2025-11-25
> 两版，agentfleet MCP 客户端对齐 2025-06-18 stdio 子集）。动机：自研客户端的升级依据
> ——sampling/elicitation/Tasks 三大新能力与 agentfleet 架构的映射。

## 一、新能力四件套

| 能力 | 语义 | agentfleet 映射 |
|------|------|----------------|
| **Sampling with Tools**（SEP-1577） | server 借客户端 token 跑自己的 agentic loop——采样请求可携带工具定义 + tool choice + 并行调用 | **缺位**——server 无法借 fleetd 的 LLM 算力（潜在：评测旁路/Judge 服务化） |
| **Elicitation + URL mode**（SEP-1036） | server 请求结构化用户输入；URL mode 带外凭据收集（OAuth/PCI——客户端不经手凭据） | **缺位**——审批门已有（I29），但 server 发起的结构化输入请求是反向通道 |
| **Tasks**（SEP-1686，实验性） | 长任务状态跟踪（working/input_required/completed/failed/cancelled）+ 主动轮询 + session 隔离 | run_steps + SSE 已是等价实现 ✓（our runs = their tasks） |
| **Streamable HTTP** | 远程 transport 主形态（SSE 降级为断连轮询） | stdio 本地子进程——远程接入时的 transport 候选 |

## 二、对 agentfleet 的对读

1. **stdio 子进程仍是本地最优**：fleetd 与 MCP demo server 同机——stdio 零网络开销；
   Streamable HTTP 是远程接入才需要的（多实例/远程 server）。
2. **Sampling with Tools 的潜在价值**：若 fleetd 作为 MCP server 暴露给外部 agent（如
   Claude Code 连 fleetd），fleetd 可请求对方 LLM 补全——评测旁路/Judge 服务化的反向
   形态。个人级暂无场景，标注为联邦方向。
3. **Elicitation ↔ 审批门**：server 发起结构化输入请求 ↔ 我们任务级审批门 + I33 工具级
   闸——语义同构（server 触发人审），但我们的闸在平台侧（可审计），Elicitation 在
   client 侧 UI——**平台侧审批门更符合管控面定位** ✓。
4. **Tasks ↔ run_steps/SSE**：等价实现确认（状态机五态与我们的 queued/running/review/
   done/failed 高度映射——input_required ↔ review）。

## 三、升级优先级建议（若接外部 MCP server）

1. **Streamable HTTP transport**（接远程 server 的前提——stdio 仅本地）。
2. **URL mode elicitation**（第三方凭据安全收集——对接外部 SaaS 工具时）。
3. Sampling with Tools（联邦 LLM 互借——最后）。

## 四、明确不抄

- 完整 spec 面面俱到实现（stdio 子集已覆盖本地 demo/演示动线）。
- Tasks 实验性能力（run_steps+SSE 等价）。
- 客户端凭证文档/扩展机制（无第三方分发）。

## 五、来源

- [MCP 规范 2025-06-18（官方）](https://modelcontextprotocol.io/specification/2025-06-18)
- [One Year of MCP：2025-11-25 发布（官方博客）](https://blog.modelcontextprotocol.io/posts/2025-11-25-first-mcp-anniversary)
- [dify-plugin-daemon（对照实现）](https://github.com/langgenius/dify-plugin-daemon)
