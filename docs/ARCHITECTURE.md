# ARCHITECTURE — agentfleet

> 面向两个读者：维护者 + 评审者。只写「为什么这么选」，不抄功能清单（功能见 README）。

## 1. 总体

```
React 管理台 (Vite+React18+TS strict+Tailwind4)
   │ /api（Vite proxy → :8090）
Go 控制面（单进程 fleetd，标准库 ServeMux）
   ├─ api        REST + SSE：路由、校验、SSE 订阅（Last-Event-ID 续传）
   ├─ scheduler  调度循环：sweep 依赖级联 → Automations 到期 → ClaimNextTask → 执行
   ├─ runner     可插拔执行器：mock（确定性剧本）/ llm（本地模型 JSON 协议）
   ├─ gateway    LLM 统一接入：探测→Ollama 原生通道→mock 降级；用量计量
   ├─ eval       LLM-as-judge：旁路执行 + 要点命中 + judge 评分
   └─ store      Store 接口：Postgres（生产）/ MemStore（演示+测试）
```

第一原则：**LLM 是不可靠依赖**。调度、状态机、评分聚合全部是确定性代码；
LLM 只做语言层的事（生成回复、judge 评语）。模型挂掉 → runner 降级 mock / judge 降级要点匹配，
平台功能完整可用。这是「管理 agent 的平台」自己必须先做到的可靠性。

## 2. 设计小节一：为什么任务队列用 Postgres SKIP LOCKED，不用 Redis

**决策**：`ClaimNextTask` 是一条事务内 SQL（`internal/store/postgres.go`）：

```sql
WITH candidate AS (
  SELECT t.id FROM tasks t JOIN agents a ON a.id=t.agent_id
  WHERE t.status='queued' AND t.visible_after<=now() AND a.enabled
    AND NOT EXISTS (SELECT 1 FROM tasks d WHERE d.id=ANY(t.depends_on) AND d.status<>'done')
    AND (SELECT COUNT(*) FROM tasks r WHERE r.agent_id=t.agent_id AND r.status='running')
        < a.max_concurrency
  ORDER BY t.priority DESC, t.id
  FOR UPDATE OF t SKIP LOCKED LIMIT 1
)
UPDATE tasks SET status='running', started_at=now() WHERE id=(SELECT id FROM candidate) RETURNING ...
```

- **依赖满足、agent 启用、并发配额、延迟可见（重试退避）四件事全在 SQL 内判定**，
  认领即原子状态迁移，不存在「取出来再检查」的竞态窗口
- `SKIP LOCKED` 让多实例并行认领互不阻塞——横向扩展是免费的
- **代价**：任务量大了以后轮询有空转成本。升级路径明确：任务表 >10 万行或 tick 空转占比高时，
  换 LISTEN/NOTIFY 或 Outbox——触发条件写在测试注释里，不是拍脑袋

**为什么不选 Redis/消息队列**：引入 Redis 意味着多一个有状态组件、双写一致性（DB 状态 vs 队列消息）、
以及「队列说跑过了但 DB 没记录」这类对账问题。控制面的任务是**强状态**的（要看板、要追溯），
任务本来就要落 Postgres——队列语义直接在状态上表达，复杂度少一整层。
（诚实的反面：如果任务改成秒级海量短任务，这个选型要重估。）

## 3. 设计小节二：为什么 v1 自研 runner，不先上 Eino/LangGraph

**决策**：`runner.Runner` 是一个单方法接口（`Execute(ExecRequest) Result`），
v1 只有两个实现：mock（确定性剧本）+ llm（本地模型 JSON 协议）。

- 平台的价值在**管控面**（队列/状态机/观测/评测），不在编排 DSL。Eino 的 Graph/Chain 抽象
  解决的是「怎么表达 agent 流程」，而 fleet 要的是「怎么治理一群 agent 的执行」——抽象层级不同
- 自研运行时把**观测切面**长在自己手里：每个 llm/tool/handoff/gate 步骤由 runner 通过
  `Recorder` 回调落库，SSE 实时推送。套第三方框架的 callback 体系做这件事，适配成本反而更高
- 本地 9B 模型的现实：原生 function calling 可靠性差。llm runner 用一次 JSON 决策
  （reply/handoff_to/needs_approval），解析失败降级纯文本——可观测、可测试、换模型不改代码
- **升级路径已铺**：F7 计划 Eino 适配器（build tag 隔离）——接口就是为这一天留的
- （诚实的反面：如果做复杂多轮工具循环，自研 JSON 协议会膨胀，届时该换。）

## 4. 设计小节三：Handoff 状态机与指令一次性消费

Handoff = 任务改派给另一个 agent 重新排队。三个不变量：

1. **防环路**：`handoff_count` 上限（默认 3），超限任务 failed 并记录 trace
2. **自交防护**：handoff 目标=自己 → 不再排队，按当前产出收尾（`handoff-self` step）。
   「交接给自己」语义上是「我的份内工作完成了」，不是失败
3. **指令一次性消费**（本项目的招牌踩坑）：交接后调度器在「原 prompt + 交接备注」的
   **最终组合文本**上，把 `handoff:X` / `交接给X` 全形态替换为 `（已由 X 接手）`。
   动机：交接备注本身就是下一棒的 prompt 通道——实测备注里的一句「交接给 fixer」
   会让 fixer 再次命中交接剧本，连环误交接直到超限（journal 2026-11-06 03:10）。
   指令是资源，消费即销毁——这条语义对 llm runner 同样成立（防止下一棒把上一棒的
   交接指令当新任务），是比「提示词写清楚」更结构化的解法

审批门同属状态机：`review` 状态阻断认领；approve 注入 `[审批记录]` 到 prompt 并重新排队；
runner 见标记走「放行后执行」分支。**审批是 prompt 改写 + 状态迁移，不是布尔位**——
留痕（谁在何时放行）和语义（放行后行为改变）都在。

## 5. 观测模型

- `runs` / `run_steps`：每次执行的全部步骤（kind=llm/tool/handoff/gate），含 input/output 摘要、
  latency、tokens。瀑布图按 latency 归一化画条
- 事件双写：`events` 表持久化（全局 seq）+ Hub 内存扇出。SSE 断线重连带 Last-Event-ID，
  服务端从表里回放缺口——**事件不丢，只可能晚到**
- 慢消费者保护：订阅通道满即踢（客户端自动重连补拉），不拖垮调度主循环

## 6. 评测模型

评测是**旁路流量**：不走调度器（不占 agent 并发闸、不进任务看板），直接经 gateway 执行。
流程：被评 agent 作答 → judge（LLM）对照要点打 0-10 分 → 报告落库。
judge 在模型不可用时降级为「要点命中启发式」，评测链路永远可演示。
已知问题与数据：作答截断（max_tokens 700→1600 后 judge 提及截断 5/8→3/8），根治方案 F9
（作答长度指令化）——**用评测数据支撑优化决策**，这正是评测存在的意义。

## 7. 存储双实现与测试策略

`Store` 接口两实现：Postgres（生产）+ MemStore（`--mem` 演示 + 全部单测）。
收益：CI 零外部依赖、测试确定性重放、新状态机行为先在 MemStore 上写测试再落 SQL。
代价：两条实现要人肉保持语义一致（ClaimNextTask 两边逻辑对齐是纪律）。
 MemStore 的锁粒度是全局 RWMutex——演示与测试规模足够，生产永远走 Postgres。

## 8. 性能基准（scripts/load.sh，--mem 模式实测 2026-11-07）

- 35 任务混合负载（30 并发灌入 + 种子审批门/依赖链）20.9s 全部消化，**吞吐 ≈1.4 任务/秒**
- 调度 tick 800ms + 事件唤醒（Wake 后派发延迟 94ms，见 F13-a）
- 瓶颈在 mock 执行的固定 sleep 与 curl 灌入开销，非调度器；纯调度吞吐远高于此
- 限流开启时（FLEET_RATE_LIMIT）按 IP 令牌桶 429，行为见 F8

## 9. 已知边界（诚实清单）

- Automations 多实例重复触发（单实例无碍；分布式锁在 Q 系列）
- MemStore 全局锁、SQLite 级并发假设
- runner 工具集 v1 只有确定性 mock 工具（text_search 等）；真实工具执行与 MCP 接入在 F4
- 评测作答截断残留（F9）；SSE 为轮询式 refetch 而非细粒度增量（前端简化取舍）
- 无认证（内网演示定位）；API key + RBAC 在 F 系列
