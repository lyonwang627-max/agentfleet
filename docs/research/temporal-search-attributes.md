# 调研：Temporal Search Attributes（可查询元数据与过滤语法）

> 2026-11-13 · 深度组件冲刺 B 切片 · 对标对象：Temporal Visibility（Search Attributes +
> List Filter 查询语法）。动机：agentfleet 查询面（ListTasks 多参数过滤 / events_query
> 增量拉取）的对读——独立参数 vs 表达式语法的取舍验证。

## 一、Search Attributes 模型

- 自定义属性必须**先在集群侧注册**才能使用；类型决定查询能力：
  - **Keyword**：精确匹配 + `STARTS_WITH` 前缀 + `BETWEEN` 后缀区间（如
    `WorkflowId BETWEEN "order-" AND "order-~"`）。
  - **Text**：Elasticsearch 标准分析器词级搜索——`=` 是 OR 匹配，部分词不匹配全词。
  - **Datetime**：支持**相对时间** `NOW() - '24h'`（单位 ns/us/ms/s/m/h/d 可组合）。
- 多数默认属性带 `Temporal` 前缀 + 非前缀别名；自定义属性同名时优先。

## 二、List Filter 语法

- 操作符：`=, !=, >, >=, <, <=, BETWEEN, IN, STARTS_WITH(Keyword only), IS NULL`
- 逻辑组合：`AND, OR, ()`（无 NOT）。
- 示例：
  ```
  ExecutionStatus = 'Failed' and CloseTime > NOW() - '5m'
  WorkflowType STARTS_WITH 'order-'
  CustomInt > 5 and CustomKeyword IN ('a', 'b')
  ```
- ORDER BY：**Temporal Cloud 不支持**；默认排序 `CloseTime DESC NULL FIRST, StartTime DESC`。
- 超 10,000 条结果建议 CountWorkflow + 分页 token。

## 三、对 agentfleet 查询面对读

| 能力 | Temporal | agentfleet 现状 |
|------|----------|----------------|
| 状态过滤 | ExecutionStatus = | ListTasks status 参数 ✓ |
| 多条件组合 | AND/OR/IN/BETWEEN 表达式 | 独立参数（label/project_id/status/agent_id）——单参数各自过滤 |
| 事件增量 | （visibility 不涉） | events_query since/kind/task_id ✓ |
| 相对时间 | NOW()-'24h' | **缺**——since 只收绝对 seq/time |
| 前缀搜索 | STARTS_WITH（Keyword） | 缺——labels 精确匹配 |
| 词级搜索 | Text 类型 ES 分析器 | 不需要 |

## 四、对 agentfleet 的借鉴

1. **I39 入池：events_query 相对时间**（S）——`since` 参数支持 `NOW()-24h` 形态（与
   Temporal 同语法），消费侧（webhook dispatcher 的 lastSeq 语义不受影响）。
2. **任务标题前缀搜索**（S）：ListTasks 加 `title_prefix` 参数（Go `strings.HasPrefix`
   内存过滤即可——个人规模无需索引）。
3. **明确不抄表达式语法**：独立参数 + 前端下拉过滤对个人级规模是更简的正确姿势
   （表达式需要词法解析器 + 注入防护）；Temporal 需要表达式是因为 visibility 面向
   海量 Workflow 的即席查询——场景不同。

## 五、明确不抄

- Elasticsearch Text 词级搜索（无 ES 依赖原则）。
- 表达式查询语言（个人级独立参数足够；表达式是海量即席查询的需求）。
- Temporal Cloud 的 ORDER BY 限制规避方案（无相关性）。

## 六、来源

- [Search Attributes（官方）](https://docs.temporal.io/search-attribute)
- [List Filter 语法（官方）](https://docs.temporal.io/list-filter)
- [Temporal CLI workflow 命令参考](https://docs.temporal.io/cli/command-reference/workflow)
