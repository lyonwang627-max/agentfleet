# 调研：结构化日志语义（Go slog / Loki / OTel Logs 对读）

> 2026-11-14 · 深度组件冲刺 B 切片 · 对标对象：Go slog 官方实践 + Grafana Loki
> labels/structured metadata + OTel Logs bridge。动机：fleetd 日志全是 `log.Printf`
> 文本——task_id/run_id/agent 嵌在消息文本里不可查询，生产化明确缺口。

## 一、核心语义共识（2025 实践）

1. **结构化键值**：日志用 key-value 字段而非消息拼接——可解析/过滤/检索（Go 官方 slog 博客）。
2. **trace 关联**：otelslog bridge 自动从活动 span context 注入 `trace_id/span_id`——
   **不要手工往每条日志抄 trace_id**；轻量替代（全套 OTel 过重时）：root 字段注入 trace_id。
3. **Loki 标签 vs 结构化元数据**：
   - **labels 索引**：只放低基数（app/env/service/level）——高基数（trace_id/user ID）
     会撑爆索引。
   - **structured metadata 不索引**：高基数（trace_id/span_id/user/session）放这里，
     LogQL 可过滤但不进索引。
   - 通过 `trace_id` 字段在 Grafana 里关联 logs↔traces（非 label 查找）。

## 二、对 agentfleet 的对读

| 维度 | fleetd 现状 | 业界语义 | 差距 |
|------|------------|---------|------|
| 日志形态 | log.Printf 文本（"[scheduler] task#5 → mock"） | slog key-value | 字段不可查询 |
| task_id/run_id | 嵌在消息文本 | 独立字段 | 检索需 grep |
| trace 关联 | 无 | span context 自动注入 | I17 层级数据已有，缺日志侧挂钩 |
| 结构化事件 | events 表（task_id/run_id 已是列） | Loki structured metadata | **事件流本来就是正确形态** ✓ |

## 三、对 agentfleet 的借鉴

1. **I41 入池：slog 结构化日志统一**（Printf → slog，字段化 task_id/run_id/agent/model/
   strategy——与 events 表字段对齐，日志与事件流可互查）。
2. **events 表 ≈ 结构化日志的正确形态已存在**：task_id/run_id/agent 都是独立列——
   日志改造向 events 的字段形态看齐即可，两套观测面可互查。
3. **轻量 trace 关联**（Reddit 路线）：不引全套 OTel Logs SDK，run 上下文里注入
   run_id 根字段（slog With）——与 I17 trace 层级、I34 CC session 关联。

## 四、明确不抄

- 完整 OTel Logs SDK + collector 管道（F12 trace 导出先行；日志桥接等真接 Loki 时再做）。
- Loki 标签体系（无 Loki 依赖；events 表已承担结构化查询面）。

## 五、来源

- [Go 官方博客：Structured Logging with slog](https://go.dev/blog/slog)
- [Dash0：OTel Logging in Go（otelslog bridge）](https://www.dash0.com/guides/opentelemetry-logging-in-go)
- [Loki：Structured Metadata](https://grafana.com/docs/loki/latest/get-started/labels/structured-metadata)
- [OneUpTime：Go 结构化日志对比](https://oneuptime.com/blog/post/2026-01-07-go-structured-logging-opentelemetry/view)
