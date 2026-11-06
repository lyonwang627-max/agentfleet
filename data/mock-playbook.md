# Mock Runner 剧本手册（data/mock-playbook.md）

> MockRunner 是 agentfleet 的确定性执行器：同样的任务文本永远走出同样的步骤。
> 三个用途：**无模型环境的产品演示**（全流程可重放）、**集成测试**（scheduler_test 断言状态机）、
> **前端 Trace 瀑布图的填充数据**。对应实现：`internal/runner/mock.go`。

## 1. 决策流水线

每个任务按以下顺序经过剧本引擎（命中即停）：

```
① plan 步（固定）                    —— 永远先落一个 llm/plan step
② 搜索步（可选，不与决策互斥）        —— 任务文本含「搜索」→ 落 tool/text_search step，然后继续判定
③ [审批记录] 命中 → 审批放行分支      —— gate/approval-passed step，任务完成（审批闭环的另一半）
④ 文本含「失败」→ 异常分支           —— 返回 error，触发调度器重试退避（测重试链路）
⑤ handoff:目标 或 交接给目标 → 交接分支 —— handoff step，Result.HandoffTo 由调度器改派
⑥ 文本含「审批」→ 审批门分支         —— gate/approval-gate step，任务进 review 等人工
⑦ default → 执行分支                 —— 80ms 延迟 + execute step，正常完成
```

设计要点：**工具步与决策解耦**（②不是 switch 分支，命中的话所有后续分支仍可触发）——
真实 agent 也是「先查资料再做决定」；分支顺序⑤先于⑥意味着带交接指令的任务先移交，
由接收方自行判断是否进审批门。

## 2. 指令通道与一次性消费（最重要的设计）

mock 剧本由**任务 prompt 中的指令词**驱动。指令一旦被消费，调度器负责把它从后续文本中剥离：

| 指令 | 谁消费 | 消费动作 |
|---|---|---|
| `审批`（触发审批门） | 人工 approve | approve 时在 prompt 追加 `\n\n[审批记录] 时间 人工放行`，重放命中③走完执行 |
| `handoff:X` / `交接给X` | 调度器 handoff | 改派时在「原 prompt + 交接备注」的最终文本上把指令词全形态替换为 `（已由 X 接手）` |

**为什么这么严**：交接备注是新的 prompt 通道——曾实测「备注里写交接给 fixer」导致 fixer 再次命中
交接分支 → 自交连环（详见 journal 2026-11-06 03:10 条目）。指令消费必须是全文本、全形态的，
否则备注通道就是指令泄漏的后门。

**与 system prompt 的关系**：mock 只看任务 prompt，不看 agent 角色设定——
「summarizer 的职责是涉发布必审批」这类约束由**任务层显式声明**（任务文本写「需要审批」）。
职责分离：角色设定（llm runner 用）/ 剧本确定性（mock 用）互不污染。

## 3. 现有种子剧本（internal/seed/seed.go）

| 任务链 | 步骤编排 | 演示点 |
|---|---|---|
| 告警处置（P8） | intake：plan→搜索→handoff→fixer → fixer：审批门 → 人工放行 → fixer：③执行完成 | 工具步、handoff、审批门、审批闭环、自交防护 |
| 周报生成（P5） | analyst：plan→搜索→handoff→summarizer → summarizer：执行完成 → 依赖解除 → 发布任务进审批门 | 双 agent 交接、跨链依赖解除、双审批门并存 |
| 修复复核（P6） | intake：handoff→reviewer → reviewer：自交防护收尾 | 自交防护（handoff 给自己 = 份内完成，非失败） |
| 晨会摘要（自动化） | cron「30 9 * * 1-5」到期自动建任务 → intake 执行 | Automations 触发链路 |
| 会失败的任务（测试） | 失败分支 ×2 → 退避重试 → failed | 重试退避、最终失败 |

## 4. 新增剧本的操作清单

1. `internal/runner/mock.go`：加分支（注意插入位置=优先级；emit 的 step kind 决定瀑布图颜色）
2. `internal/seed/seed.go`：加演示任务（prompt 用指令词编排；跨任务用 DependsOn 建依赖）
3. `internal/scheduler/scheduler_test.go`：加状态机断言（MemStore+Mock 确定性重放，无外部依赖）
4. 跑 `make test` + `make run-mem` 实弹过一遍，trace 在前端 `/tasks/:id` 瀑布图核对

## 5. 已知边界（诚实清单）

- 指令词是中文关键词匹配，任务文本里**提到**「审批」「搜索」即触发——不适合当真实执行引擎，只为确定性演示/测试
- 多实例部署下 Automation 可能重复触发（单实例演示无碍，分布式锁在 Q 系列）
- mock 不产生真实 token 用量（usage 事件为 0），用量看板演示需 llm runner
