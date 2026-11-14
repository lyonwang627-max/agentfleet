# 调研：Grafana Loki LogQL 查询语义（与 events_query 对读）

> 2026-11-14 · 深度组件冲刺 B 切片 · 对标对象：LogQL 管道查询（结构化日志查询的业界
> 标准）。动机：I41 slog JSON 落地后，日志查询面对读——events_query 的独立参数 vs
> LogQL 管道语法的取舍验证（与 Temporal search attributes 调研三重互证）。

## 一、LogQL 管道模型

**流选择器 → 行过滤器 → 解析器 → 标签过滤器**，从左到右依次执行：

- **流选择器**：labels 匹配（`{app="mysql"}`，`=, !=, =~, !~`；regex 全锚定）。
- **行过滤器**（链式，建议放管道开头——相当于分布式 grep）：`|=` 包含 / `!=` /
  `|~` 非锚定正则。
- **解析器**（查询时动态提取字段）：`| json`（嵌套键 `_` 扁平化 + `servers[0]` 数组
  访问）/ `| logfmt` / `| pattern "<_> <method>"`（`<_>` 跳过）/ `| regexp`（RE2 命名
  捕获）/ `| unpack`（OTLP 结构化元数据解包，`_entry` 替换原行）。
- **标签过滤器**（提取字段上）：类型自动推断（String/Duration/Number/Bytes）——
  `duration >= 20ms`、`bytes > 20MB` 无需显式换算；`and/or` 组合。
- 容错语义：解析失败行**不丢弃**，附加 `__error__` 标签；字段冲突加 `_extracted` 后缀。

## 二、与 SQL 式/独立参数查询的对比（三重互证）

| 维度 | LogQL 管道 | SQL 式（Temporal List Filter） | agentfleet 独立参数 |
|------|-----------|------------------------------|--------------------|
| 执行模型 | 流式短路（过滤即停） | 集合级谓词 | 内存遍历过滤 |
| 字段 | 查询时动态解析 | 预存在列 | 预存在字段 ✓ |
| 容错 | `__error__` 标签 | NULL/报错 | 解析失败回退默认 ✓ |
| 类型推断 | 自动（duration/bytes） | 显式 CAST | Go 类型系统 ✓ |

**三重互证结论**：Temporal（表达式）、Loki（管道）、独立参数——三种查询面形态中，
独立参数在「预结构化数据 + 单租户 + 无索引引擎」场景下是**实现成本/表达力最优比**。
我们的 events 表字段已预结构化（task_id/run_id/agent 独立列），管道解析器的动态提取
能力对我们是冗余。

## 三、对 agentfleet 的借鉴

1. **I39 已落地相对时间**（NOW()-30ms）——LogQL 无此概念（Loki 用绝对时间戳范围），
   我们的相对时间比 LogQL 更贴近运维查询习惯 ✓。
2. **行过滤器链式短路思想**：events_query 的过滤顺序（kind → task_id → since）已是
   短路式（逐 continue）✓——大结果集下先过滤高选择性维度即可，无需改造。
3. **`__error__` 容错标签思想**：解析失败不丢行——对齐 I28 幻觉引导（引导不丢弃）✓
   设计原则同源。
4. **唯一可借鉴小项**：`line_format` 模板重写（Go text/template）——前端事件流显示
   已各自渲染，无需后端模板化。

## 四、明确不抄

- LogQL 表达式语言（同 Temporal search attributes 结论：独立参数足够）。
- 查询时动态解析器（events 字段已预结构化）。
- Prometheus 式聚合指标面（/metrics 已手写 exposition）。

## 五、来源

- [Log queries（官方）](https://grafana.com/docs/loki/latest/query/log_queries)
- [LogQL Reference](https://grafana.com/docs/loki/latest/query/logql-reference)
- [How to Query Loki with LogQL（2025 指南）](https://grafana.com/blog/how-to-query-grafana-loki-with-logql/)
- [OneUptime：LogQL 查询指南](https://oneuptime.com/blog/post/2026-01-21-logql-queries/view)
