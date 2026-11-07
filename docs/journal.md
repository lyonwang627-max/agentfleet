# 构建日志 — agentfleet

## 2026-11-06（晚）ADR-0001 落地第一批

- **23:55 ADR-0002 模型路由设计采纳**：三粒度路由（agent 静态→步骤级 SelectFor(purpose) 分流→任务级规则打分器动态路由），风险联动（risk=high 强制 L 档+人工审批）、升降级通道（失败/停滞升档、预算余量降档，与 F5 熔断构成断路器+节流阀）、四指标评估（质量 A/B<1 分差/决策 p99/一致率>85%/升级率 5-15%）。明确不做：学习型 router（流量不足，触发条件 >1000 任务/周×两周）、token 级路由、跨云比价。F18 按 a/b/c 拆片进 ROADMAP

- **23:40 F14 + F16 落地（ADR-0001 从文档变代码）**：①**F14 进展看门狗**——watchdog.go：running 超 3min 任务摘要喂模型判实质进展，停滞→重试+task.stalled 事件；fail-open 三重保障（judge 缺失/不健康/输出异常均放行）+ 单测；与预算看门狗分工「一个看花了多少，一个看产出了没有」。②**F16 AI 预审**——POST /tasks/{id}/precheck：无工具预审（风险+产出质量），只建议不执行，人终审；实弹真模型判 `needs_human/high`（理由：草稿无执行计划、DB 变更高风险），trace 留痕 gate/ai-precheck。③修两个 bug：字符串 `[:1500]` 越界 panic（rune 安全 clip）；契约编辑误覆盖 approve 条目（即时发现恢复）。验证：go 4 包全绿 + 双功能实弹。commit 6bd5091


## 2026-11-05 首次构建（多 agent 并行开发）
- Go 控制面：store 双实现（Postgres SKIP LOCKED / MemStore）+ 调度器（优先级/依赖/并发闸/重试/handoff 状态机/审批门/失败级联）+ LLM 网关 + 评测引擎 + SSE 事件中枢
- React 管理台 7 页面构建零报错；真实运行验证任务链全走通（含自交防护 bug 修复）

## 2026-11-06 自动化切片推进

- **01:39 F1-a/2 完成**：Automations 自动化规则数据层——新增 internal/cronexpr（120 行 5 域 cron 求值器，支持 `* / , -`，dom/dow 或规则，4 组单测）+ automations 表（schema）+ 存储双实现 + 调度器每 tick 到期触发建任务。验证：go build/vet/test 全绿。遗留：F1-b（API CRUD + seed 示例 + 契约字段 + 前端展示）；注：多实例重复触发风险已留待 F1-b 用分布式锁收口
- **02:10 F1-b/3 完成**：Automations API CRUD（POST/GET/PATCH/DELETE/enable/disable，服务端校验 cron 并持有 NextRun 计算权）+ 契约 v1.1 新增 Automations 节 + seed「晨会摘要 30 9 * * 1-5」示例。**实弹验证通过**：`*/2 * * * *` 自动化到期自动建任务→调度执行 done→next_run 推进，全链路无人工干预。遗留：F1-c（前端 Automations 管理页）；多实例幂等收口改到 Q 系列一起做（单实例演示无碍）
- **02:40 F1-c 完成（F1 整体收口）**：前端 /automations 管理页（新建表单含 cron 占位示例、列表带启停/删除/上下次触发时间、15s 轮询），路由+导航接入，构建零报错（218KB JS）。实弹补充：GET 列表 ✓ / POST 非法 cron 400 ✓ / PATCH 改 cron→next_run 重算 ✓。F1 全链路（UI 建规则→后端校验→调度器到期派活→看板看结果）演示动线完整可用。下一建议：R1 种子场景丰富 或 F2 Agent 版本管理
- **03:10 R1-a/3 完成**：审批后重放语义 + 告警链全流程实弹走通。改动：①approve 时注入「[审批记录]」到 prompt，mock 剧本见该标记即确定性收尾（审批闭环的另一半）；②mock 引擎升级：工具步与决策解耦（先搜索再决策）、handoff 判定先于审批、指令一次性消费；③实弹揪出并修复 3 个真 bug：extractAfter 不跳过 marker 后空格导致目标名提取为空默认 reviewer；交接备注通道泄漏指令词（"交接给 fixer"）引发 fixer→reviewer 连环误交接——修复为在最终组合文本上做全形态剥离；mock 与 scheduler 重复记录 handoff step。验证：go 全绿（含新 TestApprovalLoop）+ 实弹 #4 完整走通「搜索→handoff→审批门→人工放行→done」。遗留：R1-b 周报链剧本、R1-c mock 剧本文档化。**本条调试过程值得单独写一篇复盘：多 agent 指令通道设计**
- **03:40 R1-b/3 完成**：舰队扩至 5 agent（+analyst 数据分析工程师、+summarizer 内容汇总与合规），周报链重写为双 agent 剧本：analyst（搜索+数据明细）→ handoff → summarizer → 审批门，与告警链通过依赖 [t1,t2] 形成跨链联动。实弹验证：#5 数据周报 analyst→summarizer 全自动 done → 依赖解除后 #6「周报对外发布」自动进 review；告警链 #6 approve→done 审批闭环复验通过。语义注记：mock 以任务 prompt 关键词驱动，agent system prompt 的审批约束由任务层显式声明（职责分离：剧本确定性 vs 角色设定）。遗留：R1-c mock 剧本文档化。验证：go 全绿 + 双链实弹
- **04:10 R1-c 完成（R1 整体收口）**：data/mock-playbook.md 剧本手册——决策流水线七步、指令通道与一次性消费语义（含备注通道泄漏教训）、种子剧本一览、新增剧本操作清单、诚实边界清单。验证：go build/test 全绿（纯文档切片）。R1 全部完成。下一建议：F2 Agent 版本管理 或 R2 评测数据集扩充
- **04:40 R2-a/2 完成**：新增「代码评审基准 v1」评测集（8 case：批量写/破坏性变更/goroutine 捕获/超时/panic 误用/SELECT */缓存双写/PII 脱敏，每 case 带评分要点）。**评测引擎首次真模型实弹**：eval#57（reviewer × 数据集2）done，score_avg 7.5 / pass 100%，judge 评语能指出缺失要点与截断位置——有真实区分度。发现：评测作答 max_tokens=700 偏小，长回答被截断影响完整性评分 → 记入 R2-b 修复。验证：go 全绿 + eval 实弹
- **05:10 R2-b 完成（R2 整体收口）**：新增「文档问答基准 v1」评测集（8 case：发布检查/限流申请/故障定级/数据分级/值班手册/SLA/权限申请/供应商安全）+ 作答 max_tokens 700→1600。实弹 eval#67：score_avg 5.5 / pass 75%，截断评语 5/8→3/8——缓解见效但未根治（1600 对长文档作答仍不足），根治方案（作答长度指令化）立为 F9。另记：eval#66 因我传错 dataset_id（30 不存在）failed——引擎对无效数据集的失败路径工作正常。验证：go 全绿 + 3 数据集列表 + eval 实弹
- **05:40 R3 完成**：README 中文完整版重写——Multica/Orca 定位对标、7 步演示动线（总览→看板→瀑布图→审批→automations→评测→用量）、架构图、6 条设计取舍速查、测试矩阵、设计原则四条。验证：go test 回归全绿（纯文档）。下一建议：R4 架构文档 或 F2 Agent 版本管理
- **06:10 R4 完成**：docs/ARCHITECTURE.md——三篇设计小节（SKIP LOCKED vs Redis：四判定单事务+升级触发条件；自研 runner vs Eino：管控面 vs 编排 DSL 的抽象层级区分+Recorder 观测切面归属；handoff 状态机三不变量+指令一次性消费踩坑）+ 观测/评测模型（截断数据支撑 F9 决策）+ 存储双实现纪律 + 边界诚实清单。验证：go test 回归全绿。P0 内容资产全部收口，下一建议：F2 Agent 版本管理（P1 首个）
- **06:40 F2-a/2 完成**：Agent 版本管理数据层与 API——agent_versions 表（UNIQUE(agent_id,version)）+ 存储双实现 + publish/list/rollback 三端点 + 契约 v1.2。设计决策：版本链只前进（回滚=旧快照覆盖+发布新版本，可追溯）；显式 publish 而非 PATCH 自动记版（避免噪声版本）。实弹：publish v1→升级→v2→rollback→v3，配置正确回滚。验证：go 全绿（含 TestAgentVersionChain）+ 实弹三连。遗留：F2-b 前端版本历史+回滚按钮
- **07:10 F2-b 完成（F2 整体收口）**：Agent 详情页新增「版本历史」面板——发布当前配置（带备注）、版本列表（版本徽章/备注/prompt 摘要/时间）、逐版回滚按钮；操作反馈走 toast 文案（已发布 vX / 已从 vN 回滚生成 vM）。前端构建零报错（221KB JS）。F2 全链路：UI 发版→后端快照→历史可查→一键回滚。下一建议：F4 MCP 工具接入 或 F3 审批备注增强
- **07:40 F3 完成**：审批门增强——approve 可带 note；意见与留痕组成 [审批记录] 段注入 prompt（runner 重放确定性消费），gate 步 approved-by-human 落产生方案那次 run 的 trace。实弹：#6 告警链 review→带意见 approve→done，意见出现在 prompt、留痕出现在 run#43 trace。语义澄清：审批留痕属于「产生方案的 run」，重放是新的 run——两段 trace 在看板上按时间线自然分开。验证：go 全绿 + 实弹。下一建议：F4 MCP 工具接入 或 F5 Token 预算熔断
- **08:10 F4-a/2 完成**：MCP stdio 客户端最小实现（internal/mcp）——JSON-RPC 2.0 换行分帧、initialize 握手（回 notifications/initialized）、tools/list 动态发现、tools/call；读循环按 id 匹配响应、notification 忽略、慢流容错。测试用 Go 标准 re-exec 模式（TestMain 环境变量分支）进程内起假 MCP server——握手/发现/调用/坏命令四场景全过，零外部依赖。设计取舍：只实现三个方法换零 SDK 依赖 + 全透明 trace 注入点。验证：go 全绿（4 包）。遗留：F4-b runner 工具注册接入
- **08:40 F4-b 完成（F4 整体收口）**：MCP 接入全链路——①demo server 抽为正式 internal/mcp.RunDemoServer（echo/now），fleetd `--mcp demo=auto` 用 re-exec 自复用运行（零额外二进制）；②mock 剧本新增 `MCP:server/tool 参数` 指令分支：真实调用 MCP server，trace 落 tool 步（含 transport/protocol 元数据）；③GET /api/v1/mcp/servers 动态发现端点 + 契约。实弹：task「MCP:demo/echo hello-fleet」→ trace `mcp:demo/echo → echo: hello-fleet`。修了两个小 bug（ref 未用变量、工具名空格截断）。验证：go 全绿（4 包）+ 实弹。下一建议：F5 Token 预算熔断 或 F6 会话回放页
- **09:10 F5 完成**：Token 预算与熔断——agents.token_budget_24h 字段（0=不限）+ AgentTokensSince（Mem 线扫/PG SUM）+ 调度器 sweepBudgets：达预算自动停用 + agent.budget_exceeded 事件（停用后不被 sweep 命中=无事件风暴；人工 enable 恢复，口径由人决定）+ run 完成自动回填 usage_events。**修两个 bug**：①mcp 单测断言未跟上 demo server 的双工具（echo+now）；②真 bug——token 累加发生在存储侧（MemStore 副本/PG SQL），调度器本地 run.TotalTokens 恒为 0，回填前必须从 store 取回最新 run（否则用量恒 0、熔断永不触发）。实弹：预算 100 的 agent 跑 6 任务累计 1680 tokens → 自动 enabled=False。验证：go 全绿（4 包）+ 实弹。下一建议：F6 会话回放页 或 F7 Eino 适配器
- **16:20 ADR-0001 + ROADMAP 扩充**：对读 dsh-superagent（2026-10-31 开源）与腾讯 LoopForge 后形成借鉴决策——新增 F14 进展看门狗 / F16 AI 预审审批门（LoopForge 独立代批）/ F17 RSI 处方闭环（焊接 eval+F2 版本管理）/ F18 模型分层路由 / F15 chat 流式，扩充 F11（上下文 Spill）与 F13（两级事件过滤）。明确不抄：投机执行（管控面无预判场景）、BYOM（单 binary 已是简化版）。两个同构暗合点入 ADR：指令通道污染（LoopForge 注入隔离 ↔ 我们的 handoff 备注泄漏踩坑）、审批零阻塞（review≠running 状态机天然实现）。ADR 见 docs/adr/0001-superagent-borrowing.md
- **15:47 手动切片**：①**优雅停机 drain**——执行 goroutine 改用独立 ctx（与轮询 ctx 解耦），SIGTERM→httpSrv.Shutdown（排空请求）→sched.Stop（停认领+wg.Wait 在途跑完）→ShutdownMCP→st.Close，实弹日志全链路验证；②**单二进制交付**——web/embed_release.go（build tag release + go:embed all:dist + SPA 路由回退 index.html）/embed_stub.go（默认空实现，日常构建无需 dist），api 在 release 模式根路径服务内嵌前端；`make release` 产出 **16MB 单 binary**，实弹：API 200 + SPA 嵌入 + /tasks 客户端路由回退全通。FDE 叙事：「一个 binary + 一个 Postgres 完成私有化交付」。③ROADMAP 新增 F10-F13（cost 核算/LLM 多轮循环/OTel/LISTEN-NOTIFY）。git：两项目 initial commit 后本批为第二次提交















## 2026-11-07 执行环境维度三模块（对标 Multica/Orca 补齐）

- **Workspaces / Artifacts / Skills 落地**：任务可绑定执行环境（对标 worktree）、runner 产出结构化产物自动落库（报告/补丁/执行日志，对标 Orca artifacts）、任务可引用沉淀经验注入执行副本（对标 compound skills）。**关键设计**：skill/workspace 注入只发生在给 runner 的执行副本，任务本体 prompt 不变——经验是参考资料不是指令，指令通道纯净性（与 handoff 指令一次性消费同一原则）。schema：tasks 加 workspace_id/skill_ids 列 + 三张新表；存储双实现；REST 全套 + SSE 事件；前端 /studio 工作台页 + 任务详情产物面板。种子：release-hotfix 环境 + 告警处置 SOP + 绑定任务链。新增集成测试 TestWorkspaceSkillArtifactFlow（注入不污染本体 prompt / 产物落库 / 引用计数）。验证：go 4 包全绿 + 实弹（workspace/skill/artifact 三列表 + 使用计数）+ 前端构建零报错

## 2026-11-07 模型可选化

- **模型动态发现 + 下拉选择**：gateway.ListModels() 从 Ollama /api/tags 动态发现本机已装模型（当前 16 个：qwen3.5/lfm25/gemma4/nimble/deepseek 云端等）+ 内置 mock；GET /models 换此数据源（不再硬编码两个）。前端 Agent 详情页 Model 字段从自由文本改为下拉框（含 provider 标注与不可用禁用态；当前值不在列表时保留显示；后端不可达降级自由输入）。这是 ADR-0002 分层路由的前置——可选模型池是路由选型的数据基础。验证：go 全绿 + 实弹发现 16 模型 + 前端构建

## 2026-11-07 F6 会话回放（切片 a）

- **F6-a/2 完成**：会话回放数据层——GET /conversations（列表带 message_count/run_count/agent_name，最新在前）+ GET /conversations/{id}（全量消息+agent 名+关联 runs）；store 双实现（Mem 内存聚合 / PG 子查询聚合）。实弹：chat 真模型对话→列表 1 条（消息数 2）→详情全量消息。遗留：F6-b 前端回放视图。验证：go 4 包全绿 + 实弹

## 2026-11-07 F6 会话回放（切片 a）

- **F6-a/2 完成**：会话回放数据层——GET /conversations（列表带 message_count/run_count/agent_name）+ GET /conversations/{id}（全量消息+agent 名+关联 runs）；store 双实现。实弹：chat 真模型对话→列表→详情全通。遗留：F6-b 前端回放视图。验证：go 4 包全绿

## 2026-11-07 F6 会话回放（切片 b，F6 收口）

- **F6-b 完成（F6 整体收口）**：/replays 回放页——左侧会话列表（agent 名/标题/消息数/run 数/时间，点击切换），右侧气泡时间线（user 右对齐蓝、agent 左对齐面板色，逐条带时间戳）+ 关联 Runs 面板（链到任务 Trace 瀑布图）。路由+导航接入，前端构建零报错（233KB JS）。F6 全链路：试聊台对话 → 会话列表 → 时间线回放 → 点 run 看执行轨迹。验证：前端构建零报错 + go 全绿（数据层上轮已实弹）

## 2026-11-07 F7 Eino 适配器

- **F7 完成**：eino（字节开源，v0.9.22）适配器落地，build tag `eino` 隔离——①EinoChatModel：把 fleetd gateway 包装成 eino model.BaseChatModel（Generate 多消息 → gateway.ChatRaw native 通道；方向是「eino 编排复用我们的网关」，降级/计量/预算全继承）；②EinoRunner：eino schema 消息流 + 同款 JSON 决策协议，`-tags eino` 构建时经 EinoRunnerFactory hook 注册；③gateway.New("mock") 语义修复：显式 mock 模型 = 强制 mock 模式（修测试 404）；④Makefile eino target。双构建（主/eino）双 vet 全过，eino 单测（Generate 消息转换 + Execute 协议）全绿。探查中学到：eino 已废弃 BindTools 改用不可变 WithTools（并发安全），适配器文档可引用

## 2026-11-07 F8 网关限流（切片 a）

- **F8-a/2 完成**：IP 级 token bucket 限流——仅变更类方法（POST/PATCH/DELETE）受限，查询不限；默认关闭（FLEET_RATE_LIMIT=次/分钟开启）；429 + Retry-After；闲置桶 10 分钟回收防内存膨胀；X-Forwarded-For 反代感知。修一个语义 bug：新建桶免费放行不消耗导致限流数偏移一次（改创建即消耗标准语义）。3 组单测：桶算法回填/IP 隔离/middleware 429+GET 不受限。验证：go 5 包全绿。遗留：F8-b 用量看板限流标红

## 2026-11-07 F8 网关限流（切片 b，F8 收口）

- **F8-b 完成（F8 整体收口）**：GET /usage 新增 rate_limit{enabled,per_minute,rejected_total}（atomic 计数，拒绝路径累加）；用量看板头部限流状态徽章——开启且拒绝>0 标红、开启无拒绝绿、未开启灰。实弹：FLEET_RATE_LIMIT=3 下连发 4 请求 → 201×3 + 429，rejected_total=1 实时反映。前端构建零报错。验证：go 5 包全绿 + 实弹

## 2026-11-07 F9 评测长度控制

- **F9 完成**：作答指令化限长（分点/每点一句/≤400 字）+ max_tokens=1600 兜底。复测 eval#77（文档问答 8 case）：截断 0/8（基线 3/8，达标），score_avg 5.5→7.8——**结论：截断误伤消除后质量评分真实回归，指令化限长优于无脑加 token 预算（低延迟+可预测形态）**。插曲：复测两次因 dataset_id 错位失败（mem 重启后 id 漂移，eval#76 failed——引擎对无效 id 的失败路径再次验证），改查真实 id 后通过。验证：go 5 包全绿 + eval 实弹对比

## 2026-11-07 F10 成本核算

- **F10 完成**：gateway/pricing.go——定价表（$/1M tokens）：本地 Ollama 恒 0（私有化叙事核心数字），云端模型 FLEET_PRICING env 覆写，未知模型恒 0（宁不显示钱不编价，面板定位量级感知非财务结算）；/usage 聚合层算 cost_usd/cost_by_model/cost_by_agent（agent 按当前模型映射，诚实近似）；看板新增 Cost 卡。单测：已知/未知/大小写/env 覆写四场景。实弹：本地模型 cost_usd=0 ✓。验证：go 6 包全绿 + 前端构建

## 2026-11-07 F11 多轮工具循环（切片 a）

- **F11-a/2 完成**：decision 协议扩展 tool/tool_args——LLM runner 从单次决策升级为「决策→工具→观察→再决策」循环（maxSteps=4 防膨胀）。关键工程决策：**runToolLoop 抽成纯函数**（decide/callTool 回调注入）——生产侧接网关+MCP 注册中心，测试侧用 fake 序列驱动四种轨迹（直达/观察回灌/步数耗尽强制收尾/工具错误转观察不中断）。修两个失误：totalTokens 初版写成包级变量（并发竞态+不复位→改 Execute 局部闭包）、测试 Fatalf format 参数类型。验证：go 7 包全绿。遗留：F11-b 上下文 Spill

## 2026-11-07 F11 上下文 Spill（切片 b，F11 收口）

- **F11-b 完成（F11 整体收口）**：上下文 Spill——handoff 组合 prompt 超 1500 rune 时压缩为「[已压缩] 原始诉求 + 历经 N 次交接 + 上一结论摘要 200 字 + run#ID 引用」，完整历史在 trace 按 ID 链式召回（prompt 是工作记忆不是归档）；多轮工具循环的观察回灌同样截断 800 字。spillIfNeeded 纯函数 2 组单测（短不压/长压缩且 run#ID、原始诉求、交接数、上一结论四锚点齐全）。F11 完整能力：多轮工具循环（a）+ 上下文治理（b）。验证：go 7 包全绿

## 2026-11-07 F13 事件驱动唤醒（切片 a）

- **F13-a/2 完成**：进程内事件驱动——Scheduler.Wake()（容量 1 非阻塞合并 chan，不丢不阻塞：合并语义由 tick 兜底）；建任务/taskAction(retry,approve)/automations 触发三处全接线。**实测派发延迟 800ms→94ms**（单测断言 700ms 内完成即证明走唤醒路径，纯 tick 至少 800ms）。race 模式 7 包全绿。多实例唤醒（PG LISTEN/NOTIFY）与两级事件过滤留 F13-b。下一建议：F13-b 或 F17 处方闭环

## 2026-11-07 F17 RSI 处方闭环（借鉴 LoopForge 外环）

- **F17 完成**：①POST /evals/{id}/prescription——失败/低分 case 聚合喂诊断模型，输出「失败模式诊断 + system_prompt_addition（行为约束级）+ rationale」，写回 report.Prescription（additive）；②POST /agents/{id}/apply-prescription——addendum 追加 agent prompt + 版本管理发布新版本（可回滚，人在环上）。**实弹质量惊艳**：处方诊断出「双写不一致时序错误」，addition 是具体行为约束（强制延迟双删/binlog 补偿/事务原子性/灰度流程）；应用后版本链 v1 可追溯。闭环：评测→诊断→处方→应用（再评测验证留给后续任务自动化）。验证：go 7 包全绿 + 双端点实弹

## 2026-11-07 F18 模型分层路由（切片 a，ADR-0002 落地）

- **F18-a/3 完成**：gateway.SelectFor(purpose)——judge 走 S 档（lfm25:latest，env 可覆写）、execute/review 默认 M/L；S 档不可用自动回退 M（只慢不错）；WithModel 派生视图不污染原客户端（并发安全）。接线：进展看门狗走 S 档（高频判断，延迟敏感）、AI 预审走 L 档（质量最敏感）；启动日志打印三档分流表。3 组单测（分档/回退/隔离）。遗留：by_purpose 用量维度、F18-b 任务级打分器、F18-c A/B 报告。验证：go 7 包全绿

## 2026-11-07 F18 任务级路由（切片 b）

- **F18-b/3 完成**：ScoreTier 纯规则打分器（priority≥8 +2、prompt>2000 字 +1、含审批/发布 +1、含 handoff +1；score≥3→L，全 0 且低优短文→S，否则 M；近期成功率<50% 直接 L——质量问题先升级）+ 失败重试自动升 L 档 + TierToModel（S/L 档经 gateway 分流，无网关回退 agent 模型）+ ExecRequest.ModelOverride + trace route 步（档位/模型/重试次数可观测）。4 组单测。插曲：一次编辑把 Scheduler struct 覆盖掉（即时发现重建）。验证：go 7 包全绿

## 2026-11-07 F18-a 收尾（by_purpose 用量维度）

- **by_purpose 完成**：usage_events 加 purpose 列（execute/chat/judge/review——路由可观测维度）；run 回填 purpose=execute、chat=chat；UsageByPurpose 聚合（Mem map 分桶 / PG GROUP BY）+ /usage.by_purpose；分桶不串单测。至此 F18-a 完整（SelectFor 分流 + by_purpose 观测）。剩余 F18-c（A/B 报告）。验证：go 7 包全绿

## 2026-11-07 F18 路由 A/B 与升级率（切片 c，F18 收口）

- **F18-c 完成（F18 三片全部收口）**：①GET /evals/compare——两次评测对比（score/pass_rate delta、quality_held 判定 |Δ|<1、verdict pass|regression|improvement、case 级结论翻转统计）；②GET /routing/stats——升级率=重试任务占比（健康区间 5-15%，三态：>30% tier-S-overloaded / <5%且样本≥20 M-underused / healthy）。单测：improvement 判定 + case 翻转计数。**诚实边界**：决策 p99 与一致率需 per-decide 延迟采集与双判基建，已标注后续。F18（ADR-0002 模型分层路由）三片全部完成。验证：go 7 包全绿

## 2026-11-07 F15 chat 流式（切片 a）

- **F15-a/2 完成**：gateway.StreamChat（native /api/chat stream:true，NDJSON 增量解析，token 数优先取服务端 eval_count；mock 逐字输出保确定性）+ POST /chat/stream（chunked text 流 + 末行 @@META@@ 协议：conversation_id/tokens/error；流内生成失败也保证会话/用量落库）。单测：chunk 内容/META 解析/双消息落库。修 3 次测试小错（未用导入×2、缺 agent 种子）。剩余 F15-b 前端 fetch reader 消费。验证：go 7 包全绿

## 2026-11-07 F15 chat 流式（切片 b，F15 收口）

- **F15-b 完成（F15 整体收口）**：试聊台 fetch + ReadableStream 消费 /chat/stream——assistant 气泡增量追加、@@META@@ 解析 conv/tokens、流式不可达自动降级同步 /chat（旧后端兼容）；"正在思考"占位仅在无增量时显示。端到端实弹（真模型）：89 字逐字流 + META(conv=83, tokens=46, error='')。插曲：实弹 404 抓到旧二进制占端口（重启即解）。验证：前端构建零报错 + 后端单测 + 端到端实弹

## 2026-11-07 Q1 API 集成测试

- **Q1 完成**：TestFullFlowIntegration——httptest 起完整 Routes（MemStore+mock 网关），HTTP 全链路 0.09s 回归：health→建 agent→建任务→等 done→trace（裸数组端点）→审批门 review→带意见 approve→done→用量回填。过程修 3 处测试自身问题（空响应 nil map panic、runs 裸数组 vs 对象解析、超时写成 1 纳秒）。验证：go 7 包全绿
