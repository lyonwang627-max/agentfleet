# 调研：Kafka 消费组再均衡（与 SKIP LOCKED 队列对读）

> 2026-11-13 · 深度组件冲刺 B 切片 · 对标对象：Kafka consumer group 协议
> （KIP-429 cooperative / KIP-848 服务端协调）。动机：agentfleet 的多实例水平扩展
> （F13-b 演进方向）该学 Kafka 的 rebalance 还是 SKIP LOCKED 已天然足够——用协议级
> 证据定夺，而非直觉。

## 一、Eager vs Cooperative（KIP-429）

- **Eager（stop-the-world）**：成员发 JoinGroup 前必须撤销全部已持有分区——同步屏障
  期间「组内无人能做任何工作」，耗时随分区数扩展。即使分区最终物归原主也全组空转。
- **Cooperative（增量两轮）**：第一轮成员保留全部分区，只撤销新旧分配**差集**；
  第二轮把刚释放的分区交给新主人。屏障没有取消，只是**移动到真正迁移中的分区上**。
- 实测：10 实例 rolling bounce 场景，eager 总暂停 37,138ms → cooperative 仅 3,522ms。
- **Sticky 是 cooperative 的前提**：若 assignor 每次全量重排（如 round-robin），
  「增量变化=整个分配」，退化成更多轮次的 eager。
- KIP-848 把协调挪到 broker 侧，进一步消除 generation/revoke。

## 二、投递语义（对读 webhook dispatcher）

- Kafka at-least-once：处理完成后才 commit offset → 崩溃在 commit 前 = 重复投递。
- agentfleet webhook dispatcher 是 **at-most-once**（轮询 lastSeq 增量，重启跳过历史
  防重启风暴）——但重试队列是 **at-least-once**（失败重投可能重复）。
- 结论：接收方幂等是两种语义共同的最终要求；我们的 HMAC 签名 + 时间戳头已给防重放
  留了口子（第 3 轮可加 delivery_id 幂等头）。**LiteLLM 调研同款结论互证**。

## 三、与 SKIP LOCKED 队列的本质差异

| 维度 | Kafka 分区独占 | SKIP LOCKED 行级租约 |
|------|---------------|---------------------|
| 多实例接入 | rebalance 协议（协调者+assignor） | 直接抢行，**天然增量** |
| 顺序保证 | 分区内严格有序 | 无顺序保证（同 key 任务可能乱序） |
| 租约粒度 | 分区（批量） | 行（单任务） |
| 故障恢复 | sticky 迁移 + changelog 恢复 | 行立即被下一个跳过者捡走 |

**定夺**：agentfleet 任务无顺序需求、单任务短时、故障即重试——SKIP LOCKED 的
「行级租约 + 争抢」是更简的正确姿势；多实例扩展**不需要 rebalance 协议**——
新 fleetd 实例直接参与 FOR UPDATE SKIP LOCKED 争抢即可。Kafka 的价值在顺序消费与
有状态流处理，我们两者皆无。

## 四、对 agentfleet 的借鉴

1. **F13-b 设计确认（非新需求）**：PG LISTEN/NOTIFY 只承担「唤醒通知」，
   认领仍走 SKIP LOCKED——多实例无需引入 rebalance/租约协议，本调研以协议级证据
   关闭了「要不要学 Kafka 分区分配」的问题。
2. **webhook delivery_id 幂等头**（第 3 轮小项）：对齐 at-least-once 生态惯例，
   接收方按 ID 去重。
3. **static membership 类比**：Kafka 用 group.instance.id 避免已知成员重启触发
   rebalance；SKIP LOCKED 天然无此问题（无分配状态）——再次确认选型优势。

## 五、明确不抄

- 引入 Kafka 本身：单机 PG 队列 + SKIP LOCKED 已覆盖需求；broker 运维成本不成比例。
- rebalance 协议/assignor：无分区概念可分配。
- KIP-848 服务端协调：同理。

## 六、来源

- [Cooperative Rebalancing（Confluent，KIP-429 权威解读）](https://www.confluent.io/blog/cooperative-rebalancing-in-kafka-streams-consumer-ksqldb)
- [KIP-848 下一代 rebalance 协议（Instaclustr）](https://www.instaclustr.com/blog/rebalance-your-apache-kafka-partitions-with-the-next-generation-consumer-rebalance-protocol)
- [CooperativeStickyAssignor 官方文档](https://kafka.apache.org/25/javadoc/org/apache/kafka/clients/consumer/CooperativeStickyAssignor.html)
- [Kafka Rebalancing 概览（Confluent）](https://www.confluent.io/learn/kafka-rebalancing)
