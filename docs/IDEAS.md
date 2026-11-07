# IDEAS — 自驱优化需求池

> **用法（给迭代 agent）**：本文件是项目自己的产品待办池。每轮「自提需求」切片：
> ①从池里挑一个 `proposed` 项（或基于对开源项目的调研新增一项，写清动机与对标）；
> ②实现 + 测试；③状态流转 proposed → in-progress → done（完成后同步 ROADMAP 若相关）。
> 提需求的纪律：每条必须有「动机（什么场景疼）」+「验收标准」+「量级估计（S/M/L）」。

## 池

- [ ] **I1 任务取消传播到在途 run** `proposed` `M`
  动机：现在 cancel 只改任务状态，已认领的 run 会继续跑完（浪费模型算力，用户在 /tasks 看到取消后仍变 done 的错乱）。
  验收：cancel 后在途 run 的 ctx 被取消，任务终态为 canceled 且不再被 runner 结果覆盖；集成测试覆盖。
- [x] **I2 Prometheus /metrics 端点** `done`（2026-11-07）
  实现：GET /metrics 手写 exposition（零 client 依赖）——tasks_by_status/tasks_total/agents_enabled/tokens_total{purpose,model}/rate_limit_rejected_total/eval_runs_total/uptime_seconds，HELP+TYPE 头按指标名去重；单测锁死关键行。压测对照可用。
- [ ] **I3 Agent 克隆** `proposed` `S`
  动意：Multica 心智——从现有 agent 派生变体调参；配合版本管理（克隆体继承最新版本快照）。
  验收：POST /agents/{id}/clone → 新 agent 配置同快照、名称自动加后缀；前端舰队页加克隆按钮。
- [ ] **I4 评测数据集导入导出** `proposed` `S`
  动机：数据集可移植（JSON 下载/上传），便于备份与分享；F17 处方闭环的再评测需要稳定数据集。
  验收：GET /eval-datasets/{id}/export 下载 JSON；POST 导入幂等；契约新增。
- [x] **I5 OpenAPI 规范固化** `done`（2026-11-07 落在 offer-agent——FastAPI 自动生成 OpenAPI 可导出固化+CI 漂移校验；agentfleet 为 Go 标准库 mux 无自动 OpenAPI，契约=手写 api-contract.md 天然固化，无需此机制）
- [ ] **I6 chat 多轮上下文压缩** `proposed` `M`
  动机：试聊台长会话会把全部历史塞给模型；复用 F11 Spill 思想——超阈值压缩旧轮次为一行摘要。
  验收：>20 轮的会话仍能正常续聊且 prompt 长度有界；单测覆盖压缩触发。
- [ ] **I7 事件订阅出站 Webhook** `proposed` `M`
  动机：FDE 集成叙事——任务 done/failed 时回调客户系统 URL（带签名重试），对接企业现有告警/工单。
  验收：workspace 级 webhook 配置 + HMAC 签名 + 失败重试退避；契约新增。
- [ ] **I8 Runner 工具池统一** `proposed` `M`
  动机：mock 的 text_search 与 MCP 工具是两套注册路径；统一成 ToolProvider 接口后 llm runner 的多轮循环可无缝换工具源。
  验收：接口定义 + 两实现迁移 + 现有测试全绿。
- [ ] **I9 前端主题切换（暗/亮）** `proposed` `S`
  动机：演示时投影仪场景亮色更清晰；设计 token 已集中在 index.css，改造成本低。
  验收：顶栏切换按钮 + localStorage 记忆；两套 token 全量覆盖无硬编码色残留。
- [ ] **I10 调度可插拔策略** `proposed` `L`
  动机：现在认领顺序固定 priority+id；把「选哪个任务」抽成 Strategy 接口（默认/fairness/deadline），为不同业务形态留扩展点。
  验收：接口 + 默认实现迁移 + 策略单测；行为不回归。

- [ ] **I11 run 步级断点续跑** `proposed` `M`（源自 langgraph-checkpointer + temporal-replay 两篇调研，见 docs/research/）
  动机：现在 run 失败只能整任务重试；借鉴 LangGraph checkpoint 恢复 + Temporal 结果重放语义——
  run 步骤标注确定性/非确定性：**确定性步（tool 调用）重试时返回已记录观察（Temporal 语义：结果重放而非跳过）**，非确定性步（llm）重新执行；trace 标注 resumed_from。
  验收：含 2+ tool 步的任务失败重试后，tool 步观察来自已记录结果（不重复调用 MCP）；跨 agent 版本（F2 version）变化的续跑请求被拒绝并提示重新评估；与 F11 Spill 组合。

- [x] **I13 任务状态漂移检测器** `done`（2026-11-07，源自 openhands-event-stream 调研）
  实现：GET /tasks/{id}/consistency——replayTaskStatus 纯函数折叠 task.update 事件（终态屏蔽后续/审批回退合法路径）派生应有状态 vs tasks.status；人为制造漂移检出 ✓ 正常零误报 ✓。测试技巧：直调 handler 时需 r.SetPathValue 注入路径参数（绕过 mux 匹配，PathValue 才有值）

- [ ] **I14 draft/published 分离** `proposed` `M`（源自 dify-draft-publish 调研，见 docs/research/）
  动机：现在 PATCH /agents/{id} 直接改运行时配置（编辑即上线，影响在途 run）；Dify 的 draft/published 分离让编辑不破坏生产。
  验收：agents.draft_config 列 + PATCH 只改 draft + POST /publish 快照生效 + draft hash 驱动「未发布变更」徽章 + rollback 先落 draft 可二次编辑（比现在多一档保守度）。

- [x] **I15 eino 回调桥接** `done`（2026-11-07）
  实现：TraceHandler（callbacks.Handler + TimingChecker，五时点签名含 StreamReader[CallbackOutput] 流副本必须 Close 的契约）→ fleetd run_steps；ctx 双通道（InitCallbacks 注入 + TraceSinkKey 传 sink）；Generate 触发 OnStart/OnEnd/OnError 切面；EinoRunner 自动挂 sink。单测：三时点断言 + Generate 全链路（-tags eino）。

## 已完成（沉淀记录）

- ~~任务级模型路由~~（F18-b，ADR-0002）
- ~~上下文 Spill~~（F11-b）
- ~~进展看门狗~~（F14）、~~AI 预审~~（F16）、~~RSI 处方闭环~~（F17）
- ~~限流~~（F8）、~~成本核算~~（F10）、~~优雅停机~~、~~单二进制交付~~
