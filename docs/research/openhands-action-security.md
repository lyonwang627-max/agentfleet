# 调研笔记：OpenHands Action 级 LLM 安全分析

> 2026-11-07 · 自驱迭代 B 类切片 · 参考：[SDK Security 指南](https://docs.openhands.dev/sdk/guides/security)、[issue #14040 分离 guardrail 提案](https://github.com/OpenHands/OpenHands/issues/14040)、[issue #10194 risk 参数提案](https://github.com/All-Hands-AI/OpenHands/issues/10194)

## 机制是什么

安全分析器在**动作执行前**评估风险：把 agent 提议的动作（如 bash 命令）分类为 LOW/MEDIUM/HIGH——高风险动作要求用户确认或直接阻断（尤其非沙箱环境）。四职责之一是 Risk Assessment：校验 LLM 声明的风险等级。

两条实现路线的演进很值得看：
1. **LLM 自标注**（当前默认）：让生成工具调用的**同一个 LLM** 在响应里自带 `security_risk` 字段——零额外成本/延迟，但既当运动员又当裁判。
2. **风险参数内嵌**（issue #10194）：给每个工具的 schema 加 risk 参数强制模型声明——同样低成本，但可信度问题同源。
3. **分离 guardrail**（issue #14040 提案）：独立的安全分析器（不同模型/独立 pass）复判——更稳健但有成本与延迟。社区共识正从 1/2 向 3 演进。

## 怎么实现

分析器接口在动作分发给 Runtime 前拦截，输出风险分级；确认策略可配置（HIGH 必确认/必阻断）。与沙箱联动：沙箱内放宽、沙箱外收紧。

## 对 agentfleet 的可借鉴点

对照 F16 AI 预审（任务级、人工终审）与 F11 多轮循环（每轮一个 decide）：
1. **粒度对齐确认**：OpenHands 是 action 级、我们是任务级——F11 多轮工具循环落地后（I8 工具池统一为前置），每个 tool 调用前的风险标签（mock 剧本已有 `needs_approval` 语义）可下沉为 action 级 gate，与任务级预审形成两层防御。
2. **「既当运动员又当裁判」的教训直接适用**：我们 F16 已选了分离 guardrail 路线（预审 ≠ 生成同一调用）——比 OpenHands 当前默认更靠前，这是 agentfleet 的领先点而非要补的短板。
3. **风险等级驱动确认策略**：`needs_approval` 目前是布尔；借鉴 LOW/MEDIUM/HIGH 三级 → 映射「直接执行/人审/阻断+说明」三策略，给 F3 审批门加分级。

## 明确不抄的部分与理由

- **自标注 security_risk 字段**：同源 LLM 自评可信度弱，且我们 F16 分离预审已解决同一问题（调研笔记结论：我们的实现比 OpenHands 当前默认更稳健——罕见领先点）。
- **沙箱联动放宽**：agentfleet 的 mock/受控工具本就无高危副作用，沙箱联动留给 I16 DockerRuntime。

## 落地建议

进 IDEAS 池为 **I18 补充项（并入 I16/F11 范畴）**：needs_approval 布尔升级为 risk_level 三级（low=直接执行/medium=人审/high=阻断+强制说明），预审输出已具备 risk_level 字段可直接联动——S 量级小改，记为 **I29**。
