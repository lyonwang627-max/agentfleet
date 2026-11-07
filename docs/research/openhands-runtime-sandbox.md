# 调研笔记：OpenHands Runtime 沙箱（执行环境隔离）

> 2026-11-07 · 自驱迭代 B 类切片 · 参考：[Runtime README](https://github.com/gso-bench/OpenHands/blob/main/openhands/runtime/README.md)、[Sandboxes 概览](https://docs.openhands.dev/openhands/usage/sandboxes/overview)、[Docker Sandbox 指南](https://docs.openhands.dev/sdk/guides/agent-server/docker-sandbox)、[SDK 论文](https://arxiv.org/abs/2511.03690)

## 机制是什么

OpenHands 的 Runtime 是「把模型动作变成真实副作用」的组件：每个会话创建**专属 Docker 容器**，容器内嵌 agent server（暴露 bash 执行/文件编辑/浏览器等 HTTP 端点），后端通过 HTTP 与之通信——宿主与不可信的模型生成动作完全隔离。三根支柱：原生远程执行 + 环境沙箱、**LLM 驱动的 action 级安全分析**、模型无关的可组合架构。Runtime 可插拔（本地 Docker / 远程云沙箱），统一接口。

## 怎么实现

会话 → 容器一一对应；容器内 agent server 进程提供端点化能力；动作经安全分析（可选 LLM 审）后在沙箱内执行；工作区与 agent-server 分离（agent-server/workspace 解耦，SDK 设计支柱之一）。

## 对 agentfleet 的可借鉴点

对照我们的 Workspaces（v1.4）：目前 workspace 只是「路径 + 分支的元数据绑定」——agent 并不真的在其中执行，执行发生在 fleetd 进程内（mock/llm runner）。借鉴方向按成本递增：
1. **Workspace 状态机**（低成本）：active/archived 已有，补 `provisioning/ready/degraded` 状态与健康检查端点——执行环境不是静态记录而是有生命周期的实体。
2. **Runtime 接口**（中成本）：把「在 workspace 中执行」抽成 Runtime 接口（LocalRuntime / DockerRuntime / MockRuntime 三实现）——MockRuntime 给演示，DockerRuntime 复用已有 docker-compose 基建。接口形状可参考 OpenHands 的 agent-server 端点分域（exec/edit/browse）。
3. **Action 级安全分析**（与 F16 AI 预审同源）：我们已有任务级预审，OpenHands 的 action 级粒度值得在多轮工具循环（F11）落地——每次 tool 调用前过一遍风险标签。

## 明确不抄的部分与理由

- **每会话一容器的默认化**：OpenHands 面向执行任意代码的 coding agent；我们的 runner 主体是 LLM 调用+受控工具，容器默认化是过度隔离。只在 DockerRuntime 实现里提供容器选项。
- **LLM 驱动的 action 安全分析作为默认门禁**：每次工具调用都过一遍模型会数倍放大延迟与成本（本地 9B 尤甚）；保持「规则 fast path + 任务级预审」的现有分层。

## 落地建议

进 IDEAS 池为 **I16 Workspace Runtime 接口**（M 量级：Runtime 接口 + Local/Mock/Docker 三实现 + workspace 状态机 + 集成测试）——这是 Workspaces 从「元数据」升级为「真执行环境」的桥，也是接真 coding-agent runner 的前置。
