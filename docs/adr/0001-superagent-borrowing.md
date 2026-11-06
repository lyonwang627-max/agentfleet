# ADR-0001: 借鉴 dsh-superagent 与 LoopForge 的决策与取舍

> 状态：已采纳（2026-11-06）｜背景：DeepSeek Super-agent 开源（2026-10-31）与腾讯 LoopForge（2026-11 精读）发布后的一周内完成对照，形成 agentfleet 的借鉴清单与明确不抄清单。
> 价值：本 ADR 是「跟踪前沿架构并在自己平台落地」的直接证据；每条取舍都带「为什么抄/为什么不抄」。

## 一、对照总表

| 机制 | dsh-superagent / LoopForge | agentfleet 现状 | 决策 |
|---|---|---|---|
| 进展看门狗 | Watchdog Session 用模型检查 Worker「实质进展」，非进程存活 | 只有资源看门狗（预算熔断）+ 死循环防护（handoff/max_steps 上限） | **抄 → F14** |
| 上下文 Inline & Spill | 局部性原理：相邻内联、超长溢出留摘要+ID、链式召回 | handoff 备注全量追加，任务 prompt 随交接膨胀（实测连环误交接的土壤） | **抄 → 并入 F11/handoff** |
| 模型分层路由 | Router 轻量模型快速决策，Worker 才上最强模型 | gateway 有模型注册表但单一模型 | **抄 → 并入 gateway**（决策走 lfm25:2.7B，执行走 qwen3.5，可实测成本分层） |
| 两级事件过滤 | 规则 Fast Path 滤噪音，LLM Slow Path 语义复核 | 事件全量扇出 | **抄 → 并入 F13**（量小，价值在架构对齐） |
| 独立审查代批（LoopForge） | 代批模型**无工具、无凭证**，评估操作风险+授权意图；「他人发言不能替当前操作人授权」 | 审批门纯人工；reviewer 只是执行角色 | **抄 → F16**（无工具预审模型出 approve/deny 建议+理由，人终审） |
| RSI 外环处方（LoopForge） | 复盘 0-100 评分 → Agent 改 SOP/角色/门禁 → 人一键应用（实测 55→96 分） | eval 引擎与 F2 版本管理各自孤立，不闭环 | **抄 → F17**（eval/失败模式 → LLM 处方 → 版本发布 → 再评测，「平台自进化」闭环） |
| 审批挂起零阻塞（LoopForge） | 需人工的单子进队列瞬间释放后台资源 | **已天然满足**：review ≠ running，并发闸只算 running，配额即释 | **不抄，写讲点**：状态机天然同构 |
| 投机执行 | 分支预测式后台预跑（报 Bug 先试修复） | 任务为明确指派的管控面，无预判场景 | **不抄**：过度设计 |
| BYOM 脑手分离 | 云端无状态 Loop + 本地轻量 daemon | 单 binary + PG 已是同思想简化版；MCP 桥接覆盖本地工具 | **不抄**：尺度不同 |

## 二、两个「同构暗合点」（架构纵深，比借鉴本身更有价值）

1. **指令通道污染**：LoopForge 的「他人发言不能替操作人授权」（注入隔离）与 agentfleet 实测踩过的「handoff 备注通道泄漏指令词 → 连环误交接」（journal 2026-11-06 03:10）是同一问题的两面——多源文本进入 prompt 都必须做权限/指令隔离。我们有踩坑→定位→修复（指令一次性消费，全形态剥离）的完整过程，比纯设计原则更有说服力。
2. **审批零阻塞**：LoopForge 作为亮点宣传的「审批挂起瞬间释放资源」，agentfleet 的状态机天然实现——review 是独立状态不占 running 配额。说明：**好的状态机设计让一些「特性」变成「免费」**。

## 三、落地排期（进 ROADMAP）

- F14 进展看门狗（与 F11 多轮循环互为安全网）
- F16 AI 预审审批门（升级 F3）
- F17 RSI 处方闭环（焊接 eval + F2 版本管理）
- F11 扩充：多轮工具循环 + 上下文 Spill
- F13 扩充：事件驱动 + 两级过滤
- gateway 扩充：决策/执行模型分层

## 四、参考

- dsh-superagent-prototype（DeepSeek Harness，MIT，2026-10-31）：docs 000-008
- 腾讯 LoopForge（github.com/Tencent/LoopForge）：RSI 双环/独立代批/审批零阻塞
- 本仓库 docs/ARCHITECTURE.md 设计小节三（handoff 状态机三不变量）
