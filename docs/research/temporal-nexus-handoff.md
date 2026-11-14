# 调研：Temporal Nexus（跨命名空间协作与 agentfleet handoff 对读）

> 2026-11-14 · 深度组件冲刺 B 切片 · 对标对象：Temporal Nexus（2024-12 GA 的跨
> namespace/region/cloud 工作流连接原语）。动机：agentfleet handoff 是同进程内
> agent 间委派——Nexus 是「跨隔离域委派」的业界标准原语，对读确认边界与演进方向。

## 一、Nexus 模型

- **三要素**：Nexus Endpoint（调用方 namespace 注册的入口）→ Nexus Service contract
  （服务契约：服务名+操作名）→ Nexus Operation（handler 侧注册的操作）。
- **调用语义两形态**：
  - **Sync operation**：短生命周期 RPC 式——调用方在超时内等待返回。
  - **Async operation**：handler 侧 workflow 承载——调用方拿 **operation handle/token**，
    可轮询/取消/收结果；操作跨 namespace/cluster **持久存活**。
- **双向流**：caller（client namespace）→ Endpoint → handler（endpoint namespace）。

## 二、对 agentfleet handoff 的对读

| 维度 | Nexus 跨 namespace | agentfleet handoff 同进程 |
|------|-------------------|--------------------------|
| 委派边界 | namespace/region/cloud 隔离域 | 同一 store 内 agent 间 |
| 语义 | sync RPC / async handle（token 轮询/取消） | 改派重排队（任务回到队列，接收方认领） |
| 持久性 | handler workflow 承载（跨域存活） | 任务本体持久（store 即持久层） |
| 失败 | caller 侧超时/取消传播 | failTask 重试语义 + I1 取消传播 ✓ |
| 契约 | Nexus Service 显式契约 | 隐式（handoff_to 名字 + 交接备注） |

**同构确认**：
1. async operation handle ↔ agentfleet 的「改派后任务在队列」——token=任务 ID，
   轮询=SSE 事件流 ✓ 形态一致。
2. Nexus Service 显式契约 ↔ 我们隐式交接备注——**差异点**：显式契约让跨团队协作有
   稳定接口；单平台内隐式够用，多平台联邦才需要。
3. 失败语义（caller 超时/取消传播）↔ I1 取消传播 + failTask 重试 ✓ 同构。

## 三、对 agentfleet 的借鉴

1. **handoff 契约显式化（可选演进）**：交接备注 → 结构化 handoff 契约（服务名+操作
   +参数 schema）——触发条件为多平台联邦需求出现；单平台内隐式+审批门足够。
2. **async handle 思想已在**：任务 ID 即 handle（SSE 轮询/事件流）——无需改造。
3. **Nexus 的 namespace 隔离思想 ↔ squad 隔离**：小队=逻辑隔离域（leader 边界），
   跨小队协作未来若需要「联邦 handoff」，Nexus 的 Endpoint 模式是参照。

## 四、明确不抄

- Nexus RPC 协议/Endpoint 注册服务（单进程内 handoff 已直通 store）。
- 跨 region/cloud 部署拓扑（个人级单实例）。

## 五、来源

- [Temporal Nexus（官方功能页）](https://docs.temporal.io/evaluate/features/nexus)
- [Nexus 架构文档（caller/handler 两面）](https://github.com/temporalio/documentation/blob/main/docs/architecture/nexus.md)
- [Announcing Nexus（官方博客）](https://temporal.io/blog/announcing-nexus-connect-temporal-applications-across-namespaces-regions-and-clouds)
- [Hello Nexus（Keith Tenzer）](https://keithtenzer.com)
