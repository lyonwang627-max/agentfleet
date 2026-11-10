# 调研：LiteLLM Router（LLM 网关的路由/冷却/降级机制）

> 2026-11-11 · 深度组件冲刺 B 切片 · 对标对象：LiteLLM Router/Proxy（业界最流行的
> LLM 网关，本机也在跑 127.0.0.1:4000）。动机：agentfleet gateway（ADR-0002 自研
> 分流/降级/计量/预算）的运行时健壮性对标——现在只有启动时 Resolve 一次探测，
> 缺「运行中模型持续故障」的治理。

## 一、路由策略谱系

| 策略 | 语义 | 备注 |
|------|------|------|
| simple-shuffle（默认，推荐生产） | rpm/tpm 加权随机 + weight 配比 | 配额用尽自动移出（rpd/tpd） |
| usage-based-routing-v2 | 当分钟 TPM 最低者 | **官方明示不建议生产**——Redis 跨实例追踪延迟显著 |
| latency-based | 最低响应者 + buffer | `lowest_latency_buffer: 0.5` 在最低延迟 +50% 内随机，防单点过载 |
| least-busy | 进行中调用数最少 | 并发感知 |
| cost-based | 最低成本（未定价按 $1 计） | 成本感知 |
| 自定义 | 继承 base + 注册 | 扩展点 |

**Routing Groups**：不同 model_name 绑不同策略——「按用途分策略」与 ADR-0002
purpose 分流（judge=S 档 / review=L 档）同构，粒度更细。

## 二、Cooldown（运行时冷却）——我们缺的那块

- `allowed_fails`（每分钟失败计数）+ `cooldown_time`（默认 5s）：超限冷却单部署。
- **429 立即冷却**（未设 allowed_fails 时）；401/404 等不可重试错误也冷却。
- 冷却粒度=单个部署（model_id hash），不影响组内其他部署；到期自动回轮换并重置计数。
- `AllowedFailsPolicy` 按错误类型差异化（如 RateLimitError 容忍 100 次）。
- `max_parallel_requests` 超限直接 429 不排队不计冷却。

对 agentfleet：gateway 目前「模型挂了反复撞」（Resolve 只在启动时跑一次）——
**I35 候选：模型级冷却**（连续 N 次失败 → 冷却 T 秒 → TierToModel 跳过该模型选同档备选），
计数器 gateway 内存态即可（单进程无需 Redis）。

## 三、Fallback 层级

- 部署 `order`（越小优先级越高）逐层升级，每层独立重试；全层耗尽才走组级 `fallbacks`。
- **Context window fallback** / **Content policy fallback** 独立路径（错误类型决定走法）。
- weighted failover：失败后同组按 weight 重选（排除已失败，`max_fallbacks` 默认 5）。
- `num_retries` 优先级链（头 > 体 > 部署 > 全局）；**provider SDK max_retries 钉 0**
  防重试叠乘 `(1+N)²`——多层层级里重试责任必须单一所有者，通用教训。

## 四、会话粘滞

`optional_pre_call_checks: ["session_affinity"]` + `x-litellm-session-id` 头：
同一会话路由到同一部署（prompt cache 友好）。与 I34 CC session 续跑、I11 重放同属
「会话一致性」家族——多部署网关的必答题，单机单模型暂无需求，记为演进注意项。

## 五、对 agentfleet 的借鉴

1. **I35 入池：gateway 模型级冷却**（allowed_fails 计数 + cooldown 跳过 + 到期回轮换；
   TierToModel 选模型时过滤冷却中模型，同档无备选回退 agent 自带——只慢不错）。
2. **重试责任单一所有者**：gateway 层若加重试，必须与调度器 failTask 重试明确分工
   （现架构：gateway 零重试、调度器统一重试——保持，LiteLLM 的叠乘教训反证现状正确）。
3. **latency-based 思想**：ADR-0002 的决策 p99 指标（F18-c 诚实标注缺采集）可借
   lowest_latency_buffer 思想做无 Redis 单机版（内存滑动窗口）。
4. **cost-based 无定价按 $1 计**：与我们「未知模型恒 0 不编价」（F10）取向相反——
   0 成本会让成本路由失效，$1 哨兵价更稳健；记录为 F10 已知权衡（我们无 cost-based 路由，不受伤）。

## 六、明确不抄

- Redis 跨实例状态（单控制面进程，内存态足够）。
- usage-based-routing（官方自认不适合生产）。
- Routing Groups 语法糖（purpose 分流已覆盖）。

## 七、来源

- [Router - Load Balancing（官方）](https://docs.litellm.ai/docs/routing)
- [Proxy - Load Balancing](https://docs.litellm.ai/docs/proxy/load_balancing)
- [Fallbacks（Provider Failover）](https://docs.litellm.ai/docs/proxy/reliability)
- [Health Check Driven Routing](https://docs.litellm.ai/docs/proxy/health_check_routing)
