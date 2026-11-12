# 调研：Grafana Tempo 采样策略（trace 成本治理）

> 2026-11-13 · 深度组件冲刺 B 切片 · 对标对象：Grafana Tempo + OTel Collector 采样体系。
> 动机：F12 OTel 导出（需 collector 环境）唯一缺的是「导出什么」的策略依据——
> trace 层级（I17）已就绪，采样策略决定观测成本与信号保全的平衡。

## 一、Head vs Tail 采样

- **Head**：trace 开始即决策——便宜简单，被丢弃的 trace 零成本；但**对结果盲**
  （错误/慢 trace 可能被丢）。
- **Tail**：看完大部分 span 再决策——**永远保留错误与慢 trace**；代价是整 trace 缓冲
  （内存/CPU）+ 一致性路由（load-balancing exporter 按 traceID consistent hashing，
  保证同 trace 的 span 到同一实例）。

## 二、Tail 策略组合（OR 语义）

`tail_sampling` processor：一个 trace 满足任一策略即采样——典型组合：
`latency`（超阈值全保）+ `status_code`（ERROR 全保）+ 基础百分比。
**decision_wait**：按 traceID 聚合 span 的缓冲窗口（示例 10s）；trace 超窗会产生
碎片化 trace（只有窗口内 span 入库）——更长窗口换更多内存，decision cache（sampled/
non-sampled 双缓存）可延续决策并降内存，但同开会固化首次决策。

## 三、两层部署

第一层：接收 + load-balancing exporter（traceID 路由）；第二层：OTLP Receiver →
Transform → SpanMetrics/ServiceGraph Connector（**必须在 tail sampling 之前**）→
Tail Sampling → OTLP Exporter。常部署为 StatefulSet 保实例身份。

## 四、对 agentfleet 的借鉴（F12 设计依据）

1. **双层观测架构**：平台自有 trace（run_steps 全量落库）+ OTel 导出通道（采样）——
   **自有 trace 是全量真相源（类比 head 之前），导出层才是采样点**。这比「trace 只进
   Tempo」更稳：观测成本可调，审计完整性不丢。
2. **天然 tail 语义**：run_steps 在 run 结束时已完整——F12 导出可按 run 粒度做 tail
   决策（无需 decision_wait 缓冲）：`failed run 全保 + latency>10s 全保 + 其余 10%`。
   agentfleet 的 run = Temporal 的 trace，天然边界让 tail 采样零缓冲成本。
3. **策略映射到既有机制**：failed 全保 ↔ I18 死信语义；latency 阈值 ↔ F14 看门狗阈值
   （同一 3min 常量复用）；rate_limiting ↔ F8 限流计数器。
4. **Prometheus metrics 先于采样生成**（connector 位置）——对应我们 /metrics 与
   run_steps 解耦（metrics 从来不受采样影响）✓ 现架构正确。

## 五、明确不抄

- 两层 collector 集群 + StatefulSet（单机导出进程即可）。
- Adaptive Traces 托管服务（SaaS）。
- decision cache（run 粒度天然完整，无碎片化问题）。

## 六、来源

- [Tempo 官方：Tail Sampling](https://grafana.com/docs/tempo/latest/set-up-for-tracing/instrument-send/set-up-collector/tail-sampling/)
- [OneUptime：Tempo tail-based sampling（2026）](https://oneuptime.com/blog/post/2026-02-09-grafana-tempo-tail-based-sampling/view)
- [Grafana Labs：水平扩展 tail sampling](https://grafana.com/blog/how-grafana-labs-enables-horizontally-scalable-tail-sampling-in-the-opentelemetry-collector)
- [OTel 采样概念](https://opentelemetry.io/docs/concepts/sampling)
