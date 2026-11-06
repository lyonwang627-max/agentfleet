# ROADMAP — agentfleet 迭代任务清单

> **开发流程约定**（动手前先读本文件）：
> 1. 从「任务队列」挑**第一个** `[ ]` 未完成任务（顺序执行，不跳号）
> 2. 验证纪律：后端改动必须 `cd /Users/lyonwang/code/agentfleet && go build ./... && go test ./...` 全绿；前端改动必须 `cd web && npm run build` 零报错
> 3. 完成后把 `[ ]` 改 `[x]` 并加完成摘要；在 `docs/journal.md` 追加记录（日期/改动/验证结果/遗留问题）
> 4. 原则：不改 api-contract.md 已有字段（只能新增）；不破坏现有测试；Go 代码遵循仓库内既有风格（标准库 mux、pgx、接口在消费者侧定义）
> 5. LLM 相关改动注意：结构化输出走 Ollama 原生 `/api/chat`（think:false + format:"json"），HTTP 客户端一律禁用系统代理

## 任务队列

### P0 内容与演示资产
- [x] R1: 丰富种子场景——seed 包再造 2 条业务任务链（「线上告警处置」「周报生成」双 agent 剧本）+ mock 确定性重放脚本 **[a: 审批后重放语义+mock 引擎升级+修复 3 个真 bug | b: 舰队扩至 5 agent，周报双 agent 链实弹走通 | c: data/mock-playbook.md 剧本手册（决策流水线/指令消费语义/新增剧本操作清单/边界清单）全部完成 2026-11-06]**
- [x] R2: 评测数据集扩充——3 个数据集各 8 case（运维诊断 v1[首建] / 代码评审 v1[R2-a] / 文档问答 v1[R2-b]），每 case 带评分要点 **[完成 2026-11-06：双新数据集实弹评测（eval#57 评分 7.5 / eval#67 评分 5.5），judge 评语有真实区分度；作答截断 700→1600 缓解（评语提及截断 5/8→3/8）未根治→立 F9]**
- [x] R3: README.md 中文完整版（产品定位/架构图/5 分钟演示脚本/Docker 一键起/设计取舍速查）**[完成 2026-11-06：含 Multica/Orca 定位对标、7 步演示动线、6 条讲点索引表、活 ROADMAP 指引]**
- [x] R4: docs/ARCHITECTURE.md 架构文档（含「为什么 SKIP LOCKED 不用 Redis」「为什么自研 runner 不用 Eino v1」「handoff 状态机」三篇设计小节）**[完成 2026-11-06：三小节均含「诚实的反面」升级触发条件；另覆盖观测模型（事件双写+Last-Event-ID）、评测模型（旁路+截断数据决策）、存储双实现纪律、边界清单]**

### P1 功能增强
- [x] F1: Automations（自动化规则）——cron 表达式定时创建任务（对齐 Multica/Orca 的自动化心智），表 + CRUD + 调度器接入 **[a: cronexpr 引擎+存储双实现+调度接入 | b: API CRUD+契约 v1.1+seed 示例+实弹(*/2 自动建任务→done) | c: 前端管理页(/automations，构建零报错) 全部完成 2026-11-06；实弹补充：PATCH 改 cron→next_run 重算 ✓]**
- [x] F2: Agent 版本管理——agents 表加 version 链，发布/回滚 API + 前端展示 **[a: agent_versions 表+存储双实现+三端点+契约 v1.2+TestAgentVersionChain | b: Agent 详情页版本历史面板（发布备注输入/版本列表/逐版回滚按钮），前端构建零报错。全部完成 2026-11-06；实弹 publish v1→升级 v2→rollback→v3 全通]**
- [x] F3: 人工审批门增强——审批时可改写 prompt 备注，审批记录落 run_steps（kind=gate）**[完成 2026-11-06：approve 可带 note（意见注入 [审批记录] 段）；gate 步 approved-by-human 落产生方案那次 run 的 trace（审批发生在该 run 之后，语义正确）；实弹验证意见注入+trace 留痕；契约 approve body 已补充说明]**
- [x] F4: MCP 工具接入——runner 工具注册支持 stdio MCP server，工具列表从 MCP 动态发现（对齐 2026 MCP 生态）**[a: internal/mcp stdio 客户端（re-exec 假 server 四场景单测）| b: fleetd --mcp 注册（auto=复用自身作内置 demo server，re-exec 模式）+ GET /mcp/servers 动态发现端点 + mock 剧本 MCP:server/tool 指令真实调用 + 实弹 task → trace `mcp:demo/echo → echo: hello-fleet`。全部完成 2026-11-06]**
- [x] F5: Token 预算与熔断——agent 级预算配置，超限自动 pause + 告警事件 **[完成 2026-11-06：agents.token_budget_24h（0=不限）+ AgentTokensSince 双实现 + 调度器 sweepBudgets（达预算自动停用+agent.budget_exceeded 事件，停用后不再命中=无事件风暴；人工 enable 恢复）+ run 完成自动回填 usage（修了 token 累加在存储侧而本地副本为旧值的真 bug——从 store 取回最新 run 再回填）；实弹：预算 100 的 agent 累计 1680 tokens 后 enabled=False]**
- [x] F6: 会话回放页——conversation 消息 + run steps 合成时间线，前端回放视图 **[a: 数据层 API | b: /replays 页（会话列表+气泡时间线+关联 runs 链 Trace）全部完成 2026-11-07，前端构建零报错]**
- [ ] F7: Eino 适配器——可选 runner=eino（字节开源框架），编译隔离在 build tag 后
- [ ] F8: 网关限流——API key 级 token bucket + 429 响应 + 用量看板标红
- [ ] F9: 评测作答长度控制——作答 prompt 加「分点作答、每点一句、总量 ≤400 字」指令 + 复测截断率（目标评语提及截断 ≤1/8）；必要时作答与 judge 分级预算（数据支撑见 eval#57/#67）
- [ ] F10: cost_usd 真实核算——模型定价表（内置 qwen3.5=0 本地 + 可配 API 价），run/用量看板从 token 数升级为成本口径
- [ ] F18: 模型分层路由（ADR-0002 完整设计已采纳，见 docs/adr/0002-model-routing.md）——**F18-a**：gateway SelectFor(purpose) 步骤级分流（判断走 S=lfm25:2.7B，执行走 M，审查走 L；改造看门狗/预审/handoff 调用点）+ by_purpose 用量维度；**F18-b**：任务级打分器（纯规则：priority/prompt 长度/risk_level/近期成功率/预算余量）+ 升降级通道（失败 2 次或停滞→升档；余量<20%→降档）+ trace 留痕；**F18-c**：路由 A/B 评测报告（质量差<1 分 / 决策 p99 / 一致率>85% / 升级率 5-15% 健康区间）
- [ ] F11: LLM runner 多轮工具循环——决策→工具→观察→再决策（max_steps 防膨胀，复用 MCP 工具池），trace 每轮落 step；**扩充（ADR-0001）：上下文 Spill——handoff/多轮后旧上下文压缩为一行摘要+run#ID 引用，按需链式召回（治 prompt 膨胀）**
- [ ] F12: OTel trace 导出——自有 run_steps 映射 OpenTelemetry span，OTLP HTTP 导出（可选项）
- [ ] F13: 调度事件驱动——tick 轮询改 PG LISTEN/NOTIFY 唤醒（对齐 ARCHITECTURE 写明的升级触发条件），MemStore 保持 tick；**扩充（ADR-0001）：加两级事件过滤——规则 Fast Path（来源/类型白名单）初筛 + LLM Slow Path 复核（借鉴 dsh-superagent）**
- [x] F14: 进展看门狗——定时扫 running 超阈值任务，run_steps 摘要喂模型判断「有无实质进展」，跑偏则触发重试/告警事件（借鉴 dsh-superagent Watchdog；与 F11 多轮循环互为安全网；现有资源看门狗 sweepBudgets 只看预算不看进展）**[完成 2026-11-06：watchdog.go（JudgeProgress 独立函数+fail-open 单测：judge 缺失/不健康/输出异常一律放行）+ 2min 流控 + task.stalled 事件 + failTask 重试；mock 环境实测不误杀]**
- [ ] F15: chat 异步流式——试聊台长回复改 SSE 流式（start/stop 异步全双工思想，后台跑 LLM 前台不阻塞）
- [x] F16: AI 预审审批门——review 状态任务先过无工具预审模型（评估操作风险+给 approve/deny 建议+理由，借鉴 LoopForge 独立审查代批：无工具无凭证、指令注入隔离），人只终审；预审结果落 trace（kind=gate）**[完成 2026-11-06：POST /tasks/{id}/precheck + 契约 v1.3 + trace 留痕 + SSE task.precheck；实弹真模型判 needs_human/high 风险理由专业；LLM 不可用 503 人工兜底；修字符串截断越界 panic]**
- [ ] F17: RSI 处方闭环——eval 报告+失败任务模式 → LLM 生成处方（system_prompt/参数修改建议）→ 人一键应用=版本管理发布新版本 → 再评测验证（焊接 eval 引擎与 F2；借鉴 LoopForge 外环复盘开处方）

### P2 工程质量
- [ ] Q1: 集成测试——httptest 起 API + MemStore 跑通「创建任务→mock 执行→handoff→审批→done」全链路
- [ ] Q2: 并发安全测试——调度器 20 任务并发跑竞态测试（-race）
- [ ] Q3: golangci-lint 配置并清零 + scripts/ci.sh（lint+test+web build）
- [ ] Q4: SSE 断线重连协议——Last-Event-ID 支持 + 前端自动重连
- [ ] Q5: 压测脚本——scripts/load.sh（hey/wrk）+ 基准数字写进 ARCHITECTURE

### P3 延伸叙事（对外分享材料）
- [ ] N2: 3 篇设计决策 ADR（docs/adr/）：队列选型、runner 抽象、观测模型

## 完成记录
（完成后登记：`- 2026-MM-DD R1 完成，新增 2 条任务链`）

### 直接开发（在线时的即时改动）
- 2026-11-06 优雅停机 drain ✅（SIGTERM→HTTP 排空→在途任务跑完→MCP 清理，实弹验证）+ 单二进制交付 ✅（go:embed + build tag 隔离，16MB binary 实弹：API/SPA 嵌入/路由回退全通，`make release`）
