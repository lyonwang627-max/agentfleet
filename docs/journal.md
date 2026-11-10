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

## 2026-11-07 Q3 + Q5 工程质量收口

- **Q3 完成**：gofmt 全量修复（14+ 文件，历次 python 编辑缩进残留）；scripts/ci.sh 四段一键校验（gofmt→vet→test -race→前端 build），零外部工具链；golangci-lint 留待有网环境（ci.sh 预留）。**Q5 完成**：scripts/load.sh 零依赖压测（curl+python），35 任务混合负载实测吞吐 ≈1.4 任务/秒，基准数字入 ARCHITECTURE §8。验证：ci.sh 全链路通过 + 压测实弹

## 2026-11-07 项目收口（转入维护节奏）

- 用户确认 A 方案：功能冻结，转入维护。最终盘点：P0/P1/P2 全收口（F12/F13-b 依赖未就绪除外），GitHub 公开（3 commit 批次：初始版本/优雅停机+单二进制/ADR+后续功能流），ADR×2，性能基准入档。cron 切维护节奏（每晚 02:30 巡检）。项目从 2026-11-05 首次构建到收口共 ~30 个自动化切片 + 5 次在线手动切片，全程原子边界无烂尾

## 2026-11-07 维护巡检（旧排程最后一响，已切维护节奏）

- ci.sh 四段全过（gofmt/vet/race 测试/前端 build）；依赖探测：docker 不可用（F13-b 继续 等）、OTLP collector 不可达（F12 继续挂起）；offer-agent pytest 全绿。无动作项——下一班：明晚 02:30 维护班

## 2026-11-07 自驱迭代切片 1（A 测试扩充）

- **eval 包测试从 0 到 6 组**：expectedPoints 三形态解析 / expectedThreshold 缺省与越界回退 / fuzzyHit 语义（精确+关键词多数）/ scoreCase 启发式主链路（mock 网关确定性）/ finishWith 持久化 / ChatJSON 契约可达。eval 是评分核心此前零测试——本切片后全项目 8 包无一裸奔。验证：go 8 包全绿

## 2026-11-07 自驱迭代切片 2（B 开源调研）

- **LangGraph Checkpointer 调研**（docs/research/langgraph-checkpointer.md）：每 super-step 落 checkpoint + thread_id 续跑 + 时间旅行；对 agentfleet 的借鉴=I11 run 步级断点续跑入池（确定性步跳过重放，与 F11 Spill 组合）；明确不抄：时间旅行 fork（状态机复杂度爆炸）。已推送 870d124

## 2026-11-07 自驱迭代切片 3（C 自提开发 I5）

- **I5 完成（教训修正版）**：初版错把 OpenAPI 导出写在 agentfleet Makefile（引用不存在的 backend/ 目录——Go 标准库 mux 无自动 OpenAPI），回滚后落到正确的仓库 offer-agent（FastAPI 天然生成）。scripts/openapi_check.py export/drift-check 双模式，实弹 18.5KB 契约快照 + 双向校验。**教训：跨仓库写 Makefile/脚本前先确认目标目录结构**。I5 状态 done（落 offer-agent；本仓库契约=手写 api-contract.md 天然固化）

## 2026-11-07 自驱迭代切片 4（offer-agent 侧 A 测试扩充 16→24）

- agentfleet 本侧无代码动作（ci 基线不变）。

## 2026-11-07 自驱迭代切片 5（B 开源调研）

- **OpenHands 事件流架构调研**（docs/research/openhands-event-stream.md）：State=事件流（event sourcing）、v1 不可变强类型事件；对 agentfleet 借鉴=I13 任务状态漂移检测器入池（events 重放 vs tasks.status 一致性——把双写妥协变成有护栏的妥协）；明确不抄全面 event sourcing（关系型查询需求主导，成本大于收益）。已推送 2b19d98

## 2026-11-07 自驱迭代切片 6（C 自提开发 I13）

- **I13 状态漂移检测器完成**：GET /tasks/{id}/consistency——replayTaskStatus 纯函数（终态屏蔽后续事件/审批回退合法折叠/无关 task 隔离）+ 一致与人为漂移双场景端点测试。技术收获：直调 handler 时 r.PathValue 为空（未经 mux 匹配），需 r.SetPathValue 注入——写进测试技巧。验证：go 8 包全绿

## 2026-11-07 自驱迭代切片 7（A 测试扩充）

- **scheduler 三个 sweep 函数直测补齐**（此前仅集成测试间接覆盖）：runAutomations 三场景（到期触发建任务+next_run 推进+未到期不重复 / 非法表达式自动禁用且不建任务）、sweepDependents 级联（前置 failed→依赖任务 failed+说明）。3 组单测全过。验证：go 8 包全绿

## 2026-11-07 自驱迭代切片 8（B 开源调研）

- **Temporal 确定性重放调研**（docs/research/temporal-deterministic-replay.md）：状态=纯函数(代码+事件日志)无需持久化、非确定性外化进 Activity、违反确定性即重放分叉；对 agentfleet 借鉴=I11 验收标准细化（tool 步「结果重放而非跳过」的 Temporal 语义 + 跨 agent 版本续跑拒绝）；明确不抄 durable execution 框架（成本论证）。已推送 4b94c13

## 2026-11-07 自驱迭代切片 9（C 自提开发 I2）

- **I2 /metrics 完成**：手写 Prometheus exposition（零 client 依赖）——任务状态分布/agents/usage 按用途与模型分桶/限流拒绝/评测数/uptime，HELP+TYPE 按指标名去重；聚合失败静默降级（metrics 不 500）。单测锁死 5 组关键行。压测对照口径就位。验证：go 8 包全绿

## 2026-11-07 自驱迭代切片 10（A 测试扩充）

- **runner 剧本分支直测 6 组**：extractAfter 空格回归锁（实弹 bug 回归）/审批放行分支/审批门分支/handoff 先于审批判定/搜索工具步与决策解耦/失败分支触发重试；**seed 冒烟 1 组**：一次 seed 产出 5 agent 舰队+4 任务+3 评测集 24 case+workspace+skill+automation，二次 seed 幂等。验证：go 9 包全绿

## 2026-11-07 自驱迭代切片 11（B 开源调研）

- **Dify draft/publish 版本发布调研**（docs/research/dify-draft-publish-versioning.md）：draft/published 两态分离 + draft hash 分叉追踪 + restore-to-draft；对 agentfleet 借鉴=I14 draft/published 分离入池（PATCH 改 draft、publish 快照生效、回滚先落 draft——比 F2 现状多一档保守度）；明确不抄 YAML+Git 多环境与应用内环境变量（规模不匹配）。已推送 6542097

## 2026-11-07 自驱迭代切片 12（offer-agent 侧 A 测试扩充 24→30）

- agentfleet 本侧无代码动作（ci 基线不变）。

## 2026-11-07 自驱迭代切片 13（B 开源调研）

- **eino Callbacks 调研**（docs/research/eino-callbacks-observability.md）：Handler 接口五生命周期时点（OnStart/OnStartStream/OnEnd/OnEndStream/OnError）+ 两级注册 + CozeLoop 父子关系保留；对 agentfleet 借鉴=I15 回调桥接入池（eino 组件事件转 run_steps，Recorder 的 eino 侧等价物）；明确不抄全局注册与外部 tracer（单控制面+零依赖约束）。已推送 1b78dfb

## 2026-11-07 自驱迭代切片 14（C 自提开发 I15）

- **I15 eino 回调桥接完成**：TraceHandler（callbacks.Handler 五时点 + TimingChecker）→ fleetd run_steps；ctx 双通道设计（callbacks.InitCallbacks 注入切面 + TraceSinkKey 私有 key 传 sink——两套上下文值空间隔离）。实现坑（读内部源码解决）：StreamReader 泛型参数是 callbacks.CallbackOutput 而非 schema.Message；TimingChecker.Needed 带 ctx/info/timing 三参；流副本必须 Close 否则整条管道泄漏（eino 契约）。单测：三时点断言 + Generate 全链路。验证：eino tag 构建/vet/测试全过 + 主构建零影响

## 2026-11-07 自驱迭代切片 15（B 开源调研）

- **OpenHands Runtime 沙箱调研**（docs/research/openhands-runtime-sandbox.md）：每会话一容器 + agent server 端点化 + action 级 LLM 安全分析；对 agentfleet 借鉴按成本递增=workspace 状态机（S）→ Runtime 接口三实现（M，I16 入池——接真 coding-agent runner 的前置）→ action 级安全分析（与 F16 同源）；明确不抄容器默认化与 LLM 门禁默认化（成本论证）。已推送 463b25e

## 2026-11-07 自驱迭代切片 16（C 自提开发 I3）

- **I3 Agent 克隆完成（后端）**：POST /agents/{id}/clone——配置全复制（含 token 预算）、名称自动去重（-clone/-clone-2）、全新版本链 v1（回溯看源 agent）、默认启用。单测：配置复制/命名去重/独立性/版本链新起 4 断言。前端克隆按钮留后续切片。验证：go 9 包全绿

## 2026-11-07 自驱迭代切片 17（B 开源调研）

- **Dify 1.19.0 嵌套追踪调研**（docs/research/dify-nested-tracing.md）：Workflow Tool 调用边界从「不透明单 span」升级为「父 span + 内部节点子 span」，嵌套与重复调用层级保持；对 agentfleet 借鉴=I17 trace 层级化入池（parent_seq + parent_run_id + 瀑布图缩进——与 I15/F12 共用数据基础）；明确不抄新旧分治迁移策略（量级不同）。已推送 46e9dc1

## 2026-11-07 自驱迭代切片 18（C 自提开发 I4）

- **I4 数据集导入导出完成**：GET export（attachment JSON，文件名自动生成）+ POST import（空 cases 400）；roundtrip 内容保真单测（points 数组/threshold 数值全保真）。服务 F17 再评测与数据集分享。验证：go 9 包全绿

## 2026-11-07 自驱迭代切片 19（A 测试扩充）

- **skills CRUD 全生命周期直测**（创建带 tags/列表带引用计数/更新/删除后 404）+ **chat 同步端点 shape 断言**（reply/conversation_id）——两处零测试区补齐。验证：go 9 包全绿

## 2026-11-07 深度冲刺切片 1（I18-a 死信队列后端）

- **I18-a 完成**：GET /tasks?dlq=true（failed=重试耗尽语义独立列表）+ POST /tasks/{id}/replay（清零重试预算重排队，与 retry 语义区分：「人工判定值得从头再跑」）。修一个防御缺口：API 层 Sched.Wake() 调用点无 nil 守卫（测试直调 handler 时 panic）——生产同样受益。单测：死信列表过滤/replay 清零/404。验证：go 9 包全绿。剩余：I18-b 前端死信区

## 2026-11-07 深度冲刺切片 2（I22 事件过滤查询）

- **I22 完成**：GET /events/query?kind=&task_id=&since=&limit=——服务端过滤（data 内嵌 task_id 也可过滤），limit 默认 100 上限 1000；单测三场景（kind/task_id/limit）。与 SSE 实时流互补（SSE 管实时、本端点管历史）。验证：go 9 包全绿

## 2026-11-07 深度冲刺切片 3（A 测试扩充）

- **CRUD 回归锁 2 组**：Automations 全生命周期（合法创建 201+next_run/非法 cron 400/PATCH 改 cron 重算/删除后 404）、Workspaces（缺 name 400/创建/归档后 active 过滤/删除）。过程教训：连续 python 字符串替换在含反引号模板串的行上翻车两次——改用 Edit 工具精准替换后一次通过。验证：go 9 包全绿

## 2026-11-07 深度冲刺切片 4（I24 并发闸超时）

- **I24 完成**：闸获取 select 加超时分支（150ms 可测覆盖/生产 60s）——超时任务延迟重排队（非 failed）+ agent.gate_timeout 事件。**架构认知（测试确立，比功能本身更有价值）**：ClaimNextTask 已在认领时强制并发检查——「runner 卡死占死并发闸」的担忧不成立（后续任务留在 queued 由看门狗/级联治理），闸超时仅覆盖认领-执行间的竞态窗口；测试因此改为手动占闸+直调 execute 精确命中。过程教训：drain 设计下测试收尾要先释放阻塞 runner（否则 Stop 死锁）；连续 Write 冲突三次——高竞争文件操作前必须 Read 最新态。验证：go 9 包全绿

## 2026-11-07 自驱迭代切片 20（A 测试扩充）

- **gateway.ChatRaw 3 组直测**：多消息透传+NDJSON/单响应解析（修测试自身错误：ChatRaw 是 stream:false 单响应语义，NDJSON 流式属 StreamChat——fake server 按语义修正）/HTTP 500 错误路径/mock 模式 fallback。ChatRaw 此前零测试（eino 适配器与多轮循环的底座）。验证：go 9 包全绿

## 2026-11-07 深度冲刺切片 5（I23 run 报告导出）

- **I23 完成**：GET /runs/{id}/export.md——renderRunMarkdown 纯函数（头部元数据+步骤表+最终产出截断）+ text/markdown attachment；单测断言头部/步骤表/产出截断六要素。验证：go 9 包全绿

## 2026-11-07 自驱迭代切片 21（A 测试扩充）

- **Agent 版本管理 HTTP 层直测**（F2 handler 此前仅 store 级覆盖）：publish 201 → list v1 → rollback 200 产生 v2 → 链 2 条最新 v2。修 2 处测试自身问题：SetPathValue 的 version 段提取（rollback 路径 version 在倒数第二段）。验证：go 9 包全绿

## 2026-11-07 深度冲刺切片 6（I21 Prompt 模板变量）

- **I21 完成**：renderTemplate 纯函数——六变量（task.title/id、workspace.path/branch/name、agent.name）；设计原则三条：未知变量原样保留（写错被看见）、无模板快路径零开销、渲染只发生在执行副本（与 skill/workspace 注入同通道）。5 组单测。验证：go 9 包全绿

## 2026-11-07 深度冲刺切片 7（I19 API Key 认证）

- **I19 完成**：api_keys 表（SHA-256 哈希库存）+ X-API-Key 中间件（渐进语义：表空开放→有 key 强制校验；metrics/health/管理端点豁免）+ 管理端点三件（创建返回明文一次/列表/撤销）+ **限流键升级接线闭合**（认证后按 key 名限流，F8 的升级路径兑现）。修 3 处实现期失误（mem store 初始化漏 apiKeys map→nil map panic；AuthProgressive 测试的中间件调用方式与类型引用）。单测：七步渐进语义全链。验证：go 9 包全绿

## 2026-11-07 自驱迭代切片 22（A 测试扩充）

- **StreamChat 回归锁 4 组**：NDJSON 增量拼接/eval_count 优先/无 eval_count 回退行计数/HTTP 500/mock 逐字。**抓到真 bug：done 行不带 eval_count 时行计数漏计**（tokens++ 在 Done 检查之后）——移到 Done 检查前修复。StreamChat 此前零测试（F15 底座）。验证：go 9 包全绿

## 2026-11-07 自驱迭代切片 23（B 开源调研）

- **OpenHands action 级安全分析调研**（docs/research/openhands-action-security.md）：LLM 自标注/风险参数内嵌/分离 guardrail 三路线演进；对 agentfleet 的结论=**F16 分离预审比 OpenHands 当前默认（自标注）更稳健——罕见领先点**；I29 审批风险分级入池（needs_approval 布尔→三级策略，预审 risk_level 可直接联动）。已推送 7e34fe4

## 2026-11-07 深度冲刺切片 6（I29 审批风险分级）

- **I29 完成**：Task/Result 加 RiskLevel（low/medium/high）+ mock 按关键词定级（不可逆操作→high）+ 调度器 review 门带 risk 标注 + **high 任务 approve 无意见 400**（强制留痕）+ precheck risk=high 写回 task（F16 联动）。**顺手修一个测试基建缺陷**：Server.Hub nil 时直调 handler panic——加 s.publish nil 守卫封装并统一全部子文件调用点。单测：三级策略断言。验证：go 9 包全绿

## 2026-11-07 自驱迭代切片 24（A 测试扩充）

- **I29 调度器侧分级策略回归锁**：TestRiskLevelReviewGate——高风险/中风险双任务各自进 review 后，断言 gate step 的 Input.risk 标注（high/medium 分别验证）+ task.update 事件流带 risk_level=high。修 1 处测试类型断言（Event.Data 是 any 接口，需 json round-trip 后取 map）。验证：scheduler 包测试全绿（含此新组）

## 2026-11-07 补记：修 publish 无限递归

- 上一轮的 python 正则把 publish 方法**自身的** s.Hub.Publish 也替换成了 s.publish（无限递归 → 栈溢出）。教训：**批量替换调用点前，先把定义处排除**（或替换后立即跑构建）。修正后 9 包全绿。

## 2026-11-07 自驱迭代切片 25（B 开源调研）

- **Celery revoke/terminate 语义调研**（docs/research/celery-revoke-semantics.md）：两档撤销（集合跳过 vs 信号终止）+ 时序竞态 + 社区三原则（协作式取消/幂等/优雅关闭）；**I1 取消传播方案据此定稿**——CancelCheck 注入 + 工具步边界检查 + ErrCanceled 不重试 + DB 二次确认，明确不抄 terminate（无进程可杀）与广播撤销集合（DB 状态天然持久）。已推送 2be81b0

## 2026-11-07 深度冲刺切片 7（I1 任务取消传播）

- **I1 完成（按上轮 Celery 调研定稿方案实现）**：ExecRequest.CancelCheck（调度器注入——DB 重取任务状态，认领-取消竞态二次确认）+ llm runner 每轮 decide 前检查（工具步边界退出）+ mock 入口检查 + ErrCanceled 专门分支（不重试不告警，run 收尾带 canceled 标注）。单测：slowRunner 两步之间取消传播（0.02s）+ mock 入口取消。「取消后仍变 done」的错乱从根上修复。验证：go 9 包全绿

## 2026-11-08 自驱迭代切片 26（A 测试扩充）

- **I1 端到端断言补齐（调度器级）**：TestCancelPropagationEndToEnd——cooperativeRunner 进入步边界等待区后 cancel → runner 经 CancelCheck 协作退出 → 任务终态 canceled 且 200ms 窗口内无翻转（确认 failTask 不介入）→ run 带 canceled 标注。上轮只有 runner 单元级，全链路语义现在锁死。验证：go 9 包全绿

## 2026-11-09 前端补全四连（手动切片）

- **I18-b/I3-b/I23-b/I22-b 一轮拉通**：任务看板加「看板/死信队列」双视图（DLQ 列表+重放按钮）、舰队卡片克隆按钮、run 面板导出报告链接、事件流 kind 筛选下拉。后端四端点此前已就绪，本次全部接通 UI。实弹：DLQ 36 条 / clone→intake-clone / export.md 报告完整（token/时间/步骤表）/ query 过滤正常。前端构建零报错。commit 94191cd

## 2026-11-09 I30 小队模式（对标 Multica squads）

- **源码调研驱动**：克隆 multica 仓库读 squad.go/squad_briefing.go——五核心决策（leader 只协调不动手协议/@mention 结构化分派/Roster 技能注入/每次触发强制 activity 记录/停等唤醒模型），agentfleet 取其三（协议/Roster/事件闭环）不抄其二（mention://协议——我们复用 handoff 通道；originator 门控——单用户平台无需求）。
- **实现四件**：①squads/squad_members 表 + CRUD（leader FK agent、成员混编、幂等）②leader briefing 组装（squadOperatingProtocol 中文版五职责 + 花名册含成员技能 + 小队指令段）③任务创建 squad_id 接线（缺省 leader、非 leader 400）④**成员完成自动 re-trigger leader**（跟进评估任务带上下文 + Wake 即时派发；originalAgents 内存表跟踪 handoff 前初始 leader）。seed 加「告警响应小队」示例。端到端测试全链路 1.82s。
- 教训：mem store 新增 map 字段必须同步 NewMemStore 初始化（第三次踩 nil map）。

## 2026-11-09 I31 群聊会话（共享上下文 + @ 唤醒）

- **对标 Multica 群聊内核的轻量实现**：POST /chat/room——一条共享消息流，@agent 名即唤醒（中文名正则含 CJK），该 agent 读全量时间线以自己人格回帖（message.agent_name 记录发言人）；@all 全员唤醒；**agent 回帖互 @ 链式触发**（深度≤2、单轮≤4 防爆炸）。conversations.mode=room / messages.agent_name 两列 additive。测试：@解析/全链路（双 @ 双回帖/续聊共享时间线/链式为设计行为）。Chat 页前端接入留下轮。9 包全绿

### I31 前端 + 实弹补充

- **/room 群聊页**：@ 快捷条（点击插入 mention/@all）、共享时间线气泡（各 agent 名字着色区分）、唤醒数徽章。构建零报错。
- **真模型实弹（14.4s）惊艳**：用户只 @intake → intake 读时间线定位后**主动 @fixer 链式唤醒** → fixer 回帖确认修复动作并尝试 @ops（超单轮上限被防爆炸截断）——agent 间自主协作真实发生。修一次路由注册丢失（python tab 替换在 api.go 失灵，Edit 补上——同 App.tsx 教训）。

## 2026-11-09 Multica 核心功能全量移植（M 系列）

- **后端（9 包全绿）**：①Projects（项目分组 + repos 上下文 + 任务计数）②Labels（任务标签 + label/project_id 过滤）③TaskComments 评论线程（人评论 / @agent 唤醒其以任务+线程上下文回帖，单评论≤2 唤醒）④Steer 运行中插话（steer_note 原子取清 + runner 决策边界注入 + 评论留痕 + 仅 running 可插 409）。
- **前端**：lucide-react 图标全量接入（10 项导航带图标）；/projects 项目页（建/删/repos 徽章/任务数/查看项目任务）；任务详情新增评论线程（头像区分人/agent）+ running 时 Steer 插话条。
- **真模型实弹**：项目创建 ✓ / label 过滤 ✓ / 评论 @reviewer → 「评分 95 分，优秀」以任务上下文回帖 ✓（线程 2 条）。

## 2026-11-10 I20 任务依赖 DAG 全量收口（C 切片，后端+前端）

- **后端（c4e3a6e）**：detectCycle DFS 三色 + validateNoCycle（防御深度——创建时序决定传递环不可达，deps 只能引用已存在任务）+ GET /tasks/{id}/dag（子图节点/边 + cycle 真算）+ **createTask 缺失依赖 400**（防任务永久 blocked 静默卡死）+ deps 去重保序 + TaskPatch.DependsOn（mem/pg 双实现）。回归锁：算法单测 + HTTP 级（子图/真环标记/400/去重）。
- **前端（1d5bef4）**：DepGraph SVG——最长路径分层（依赖在上任务在下）+ 贝塞尔连线 + SF 状态色 + fitLabel 像素预算截断（CJK≈11px/ASCII≈6.2px，getComputedTextLength 实测零溢出）+ cycle 红环警示。**顺手抓到 TaskDetail 钩子违规真崩溃**：loadComments 的 useEffect 在早退 return 之后（ba05750 引入），loading→loaded 切换钩子数变化整页白屏——移至早退前修复。
- 实弹：fleetd dag 端点 3 节点 2 边 ✓；浏览器验收渲染正确、控制台零错误 ✓。

## 2026-11-10 A 切片：llm.go Execute 全链路直测（33.2%→65.2%）+ 两处 -race 竞争修复

- **LLMRunner.Execute 8 组直测**（此前 0%）：mock 网关 Fallback 注入逐轮控制 ChatJSON——finish 留痕（decide 步 steps/observations/tokens 合计）/非 JSON 原文降级/工具循环失败观察回灌（第 2 轮 user 含观察）与 tool 步留痕/步数耗尽强制收尾/取消传播（ErrCanceled + llm-failed 步 + LLM 不被调用）/插话注入当轮/长回复>400 rune 固化 report 产物/buildSystemPrompt 双形态。
- **抓到真 bug：SteerDrain 无 nil 守卫**（llm.go——cancelCheck 有守卫 steerCheck 没有，直调 ExecRequest 不带插话源即 nil 解引用 panic）。修齐。
- **-race 抓到两处既有竞争**：①cancel_test 裸 bool（测试 goroutine 写 canceled、runner goroutine 读）→ atomic.Bool；②gate_timeout_test 与在途 execute goroutine 并发改写包级 gateAcquireTimeout → atomic.Int64（纳秒）。全量 9 包 -race 全绿。

## 2026-11-10 C 切片：I6 chat 多轮上下文压缩（commit 6f4d925）

- **诊断修正动机**：实际缺陷与 IDEAS 描述相反——sync/stream chat 此前每轮只发当前消息（模型无多轮记忆），群聊 buildRoomPrompt 才是真·全量无界。正解=补历史 + 压缩一起做。
- chatHistoryTranscript 纯函数：阈值 20/保留 12，较早消息确定性折叠 40 rune 单行（非模型摘要，F11 Spill 同原则）；三处接线（sync/stream/room，PromptTokens 按有界时间线计量）。单测 6 组含 60000→<6000 rune 有界证明与 mock 回显多轮记忆。
- 真模型实弹（qwen3.5，conv#86）：第 1 轮埋暗号 → 第 2 轮准确召回「gamma55」——多轮记忆端到端 ✓。
- 顺手补登 I1 状态 done（a15f77c/323a212 此前漏登记）。适配 TestChatStreamChunksAndMeta 断言（prompt 形态演进为时间线）。

## 2026-11-10 B 调研：AutoGen 群聊编排（docs/research/autogen-group-chat.md）

- 三种团队形态（RoundRobin 固定轮转 / Selector LLM 选人 / Swarm handoff）+ selector_prompt 三占位符（participants/roles/history）+ 双钩子（selector_func 全接管可回退、candidate_func 候选集收窄）+ Termination 体系（五种条件、|/& 组合、自动 reset、stop_reason 可观测）。
- **对 agentfleet 五点借鉴**：候选集收窄→I32 入池（@all 智能选人，I30 花名册数据已就位）；Termination 组合+stop_reason（防爆炸从硬编码常量升级为可观测终止原因）；woken 去重与 allow_repeated_speaker 同构确认；Steer↔ExternalTermination 同族确认；description 即选人依据验证花名册路线。
- 明确不抄：LLM selector 替代显式 @（派发可审计性）、Swarm 形态（handoff 状态机已覆盖）、组件序列化框架。

## 2026-11-10 C 切片：I32 群聊候选集收窄（@all 智能选人）

- selectRoomAgents：ChatJSON 选人器（roster 数据复用 I30 花名册 name:role+提示词头），@all 从全员唤醒改为 ≤2 名智能选人（名单外过滤/去重），失败回退全员兜底；显式 @ 不经此通道（派发可审计性）。响应 additive 字段 selection 带理由。
- 单测 4 组全过 + 9 包 -race 全绿。真模型实弹（qwen3.5）：@all 数据库连接池告警 → 选中 intake（理由=值班职责）→ intake 链式 @fixer → fixer 幻觉 @ops 被安全忽略——智能选人+链式协作+名字防御三层语义一次验证。

## 2026-11-10 A 切片：eval 引擎编排层直测（50.6%→84.4%）

- Start（异步评测入口）此前 0%：3 组直测——满分路径（要点取题目子串 → mock 回显全命中 → avg=10/pass=100 落库）、agent 缺失 failed（无报告）、空数据集 failed；waitForStatus 轮询终态（Start 是 goroutine）。round1 四舍五入边界 4 组。finish failed 路径随失败场景覆盖。
- 9 包 -race 全绿。

## 2026-11-10 C 切片：I9 前端主题切换（暗/亮）

- 亮色 token 全量覆盖（macOS light 中性梯 + SF 语义色亮版）+ **复合层变量化**（glass/hairline/row-hover/scroll-thumb/drop-shadow 五变量，@theme 定义暗默认、[data-theme=light] 覆写）——全部工具类经 var() 级联，组件零 token 改动。
- index.html 首帧前内联初始化（localStorage af-theme，防闪烁）；Layout 顶栏 Sun/Moon 切换按钮；violet 徽章亮暗自适应。
- **回归自纠**：build 抓到 1d5bef4 钩子修复时误删的 viewTrace（当时 tsc 退出码被管道掩蔽：`tsc | head; $?` 读的是 head 的退出码——校验编译必须 `tsc; $?` 直连）已恢复。
- 双主题浏览器验收：暗色零回归、亮色白底浅面板发丝线正确、localStorage 刷新持久 ✓。

## 2026-11-10 A 切片：gateway native（Ollama 原生通道）直测（68.2%→84.8%）

- 诊断：跨包归因掩蔽（Chat/ChatJSON 在 runner/api 层已被 mock 路径驱动），真缺口是 **native() 生产主通道全项目零测试**（stream/think/format 契约、HTTP 错误路径从未锁定）。
- httptest 假 Ollama 端点 6 组：成功解析（content/eval_count）+ 请求契约（stream:false/think:false/num_predict 透传/model）/ChatJSON forceJSON→format:"json"/HTTP 500/非法响应体/连接拒绝/Healthy+mock Chat 形态。
- mcp RunDemoServer 0% 排除：re-exec 子进程覆盖率假象，TestClientHandshakeListCall 已端到端驱动。
- 9 包 -race 全绿。

## 2026-11-10 C 切片：I14 draft/publish 分离第一轮（后端，M 之 1/2）

- **动机兑现**：PATCH 此前直接改线上配置（编辑即上线，影响在途 run）。新语义：PATCH 只进草稿（UpdateAgentDraft——首次从 live 快照初始化后合并），POST /agents/{id}/publish 落线（草稿→live→版本快照，F2 链语义保留：无草稿=纯快照），rollback 覆盖 live 后清草稿回干净态。
- store 三方法 additive（mem/pg 双实现：draft_config JSONB 列 + agentCols/scanAgent 扩展）；model.Agent.DraftConfig additive。
- getAgent 响应带 draft_config + has_unpublished_changes（前端徽章留第 2 轮）。
- 单测 4 组：PATCH 不落线/publish 落线+版本/无草稿纯快照/rollback 清草稿。9 包 -race 全绿。
- 教训复现：直调 handler 需 SetPathValue（第三次踩，已固化为测试注释惯例）。

## 2026-11-10 B 调研：LangGraph interrupt/resume（docs/research/langgraph-interrupt-resume.md）

- interrupt(payload) 抛可恢复异常 + Command(resume) 值成为 node 内返回值的契约；resume=node 从头重放 → 四条纪律（副作用幂等/多 interrupt 按索引/禁裸 try/禁循环 interrupt）。
- 三种 HITL 模式（Approve/Review&Edit/工具内 interrupt）——工具内 interrupt 是粒度最细形态。
- **对 agentfleet**：粒度下沉缺口→I33 工具步级审批入池（risk 分级下沉到工具调用，approve 带参数修正）；I11 验收补强（恢复=重放→副作用幂等，与 Temporal 双源互证）；Steer↔resume 值同构确认。
- 明确不抄：动态图框架/thread_id 持久化（DB 即 checkpointer）/response_schema 校验。

## 2026-11-10 C 切片：I14 draft/publish 第 2/2 轮（前端收口）

- AgentDetail：配置表单以草稿值为准（draft_config 驱动初始化——编辑所见即草稿）+「未发布变更」琥珀徽章 + 「编辑进草稿」提示语 + 保存反馈改「已存草稿」。
- 版本面板：dirty 驱动「有草稿待发布」徽章 + 按钮文案「发布当前配置」↔「发布草稿」切换 + 发布后联动刷新 dirty。
- 真机闭环（fleetd 重启 + 实弹）：PATCH 草稿 → live 不变（curl 双查证）→ 页面草稿态三件套呈现 → 点发布 → v1 落线（版本链 config=草稿值）→ dirty 清零 → 徽章消失按钮复位。
- 前端构建零报错（tsc 直连校验）。I14 整体 done。

## 2026-11-10 A 切片：watchdog sweep 分支直测（F14 补齐）

- sweepProgress 此前 26%：3 组直测——阈值内跳过（即使 judge 判停滞也不打扰）/超阈值停滞（task.stalled 事件 + failTask 重试语义 queued-or-failed + error 带停滞说明）/fail-open（judge 不健康放行）。digestSteps 0%→3 断言（空兜底/行格式/超 1200 rune 截断）。
- 9 包 -race 全绿。

## 2026-11-10 C 切片：I33 工具步级审批第 1/2 轮（LLM runner 闸）

- 高风险工具（I29 同源关键词）在 callTool 执行前拦截：ToolGateError 哨兵中止 runToolLoop（语义扩展：callTool 可中止循环）→ Execute 映射 NeedsApproval/high——**不是失败，任务进 review 门**；gate 步 tool-approval-gate 带工具/参数快照落 trace。
- 放行契约：approve 审批意见必须提及工具名（防一次审批放行全部工具）；F3 审批记录注入 task prompt → 重跑时闸自然放行，参数修正经意见注入由模型消费（与 F3 审批闭环同机制，零新基建）。
- 单测 4 组：高风险拦截/低风险回归/批准后放行（观察回灌）/判定表。9 包 -race 全绿。第 2 轮：mock 剧本路径 + reject 留痕。

## 2026-11-10 A 切片：evals HTTP 全生命周期直测（handler 0%→覆盖）

- createEval/listEvals/getEval/listDatasets 此前 0%：一条链直测——缺参 400 / 发起 201 running / 轮询终态 done（mock 引擎确定性满分）/ 报告形状（cases/score_avg）/ 两列表端点 / 404。Eval 引擎异步 Start 的 HTTP 编排面锁定。
- 9 包 -race 全绿。

## 2026-11-10 C 切片：I33 工具步级审批第 2/2 轮（整体收口）

- mock 剧本工具路径同闸：高风险剧本工具（MCP:server/tool）执行前拦截进 review 门；审批记录提及工具名后放行真实调用。
- cancel-on-review = 审批拒绝语义：gate-reject 步留痕（与 approved-by-human 对称），非 review 取消不产生留痕。
- 单测 3+1 组；9 包 -race 全绿。**I33 整体 done：审批粒度双层化（任务级 I29 门 + 工具级本项）**。

## 2026-11-10 A 切片：runs/artifacts/models/mcp-servers 观测端点直测

- getRun（run trace 形状：steps 内嵌 + **AppendStep 累计 run.TotalTokens 语义锁**）/listArtifacts（列表不带 content）/getArtifact（详情带 content）/models（动态发现含 mock）/mcp/servers 形状 + 404 路径——观测面 handler 0% 补齐。
- 9 包 -race 全绿。

## 2026-11-10 C 切片：I17 trace 层级化第 1/2 轮（数据层）

- 两列 additive：run_steps.parent_seq（循环观察步挂当轮 decide 下）+ runs.parent_run_id（handoff 链跨 run 成链）；schema/mem/pg 双实现（pg 三处 scan 扩展）。
- LLM runner 发射改造：emit 返回落库步（AppendStep 回填 Seq 可读）；工具循环每轮先落 decide-turn-N 步、tool 观察步挂其下——层级语义对齐 Dify（观察步是决策步的子 span）；汇总 decide 步保持顶层。
- handoff 链：同任务第 2+ 次 run 指向前一 run（ListRuns 首条）。
- 单测 2 组：嵌套（decide-turn-1 顶层→tool 挂其下→汇总顶层）/平铺（无循环全顶层）。9 包 -race 全绿。前端瀑布图缩进留第 2 轮。

## 2026-11-10 C 切片：I17 trace 层级化第 2/2 轮（前端收口，整体 done）

- TraceView 按 parent_seq 建树（深度优先展开 + 孤儿步容错顶层追加）：子步 ml-6 缩进 + info 左色条 + ↳ 前缀 + 提示语「缩进行为子步骤」；types.ts additive（parent_seq/parent_run_id）。
- Runs 列表 handoff 链徽标 ↳ #父run，点击直接展开父 run trace（跨 run 跳转）。
- 真模型实弹（qwen3.5 + LLM runner agent）：run#95 层级 decide-turn-1(seq2)→tool(parent:2)→decide(seq4) 顶层；浏览器截图验收缩进渲染。I17 整体 done。

## 2026-11-10 C 切片：I28 步骤级 LLM 路由第 1/2 轮（工具能力清单）

- ToolCatalog：动态发现已注册 MCP 工具（name+描述 60 rune 摘要），无 server 空清单降级；decide user 注入「可用工具」段——LLM 按能力描述选工具（eino ADK supervisor 思想下沉）。与 I33 闸天然兼容（清单是提示，闸在执行时拦）。
- **实弹发现（qwen3.5）**：清单注入后模型仍幻觉编造 server 名（mcp:system/clock 非 mcp:demo/now）——未注册报错已天然转观察回灌下一轮，但小模型遵循度有限；第 2 轮：强约束提示 + 幻觉引导重试。
- 单测 2 组；9 包 -race 全绿。

## 2026-11-10 A 切片：cronexpr Next 跳转分支直测（86.1%→97%+）

- 5 组边界：跳月（1 月任务跨年命中 2027-01-01）/跳日（1 号任务 11→12 月）/小时推进（同日 8:30→9:00）/无匹配零值（2 月 30 号 4 年搜索耗尽）/闰年 2-29（2028 命中）。
- 9 包 -race 全绿。

## 2026-11-10 C 切片：I28 步骤级 LLM 路由第 2/2 轮（整体收口）

- ToolKnown（未注册引用判定，60s TTL 缓存 + toolKnownFn seam）+ callTool 幻觉拦截：不存在的工具不进闸不执行（审批不存在的工具无意义——语义排序：幻觉判定先于风险闸），转引导观察回灌（可用清单+重新决策指令），tool 步 output.hallucination 标记；落步统一 onToolTurn（修一个自引入的时序 bug：callTool 内 emit 与 onToolTurn 重复乱序）。
- 强约束文案注入清单段。**对照实验（qwen3.5 同任务）**：改造前幻觉 system/clock 失败 → 改造后正确选 demo/now 返回真实时间——提示工程修正闭环。
- 修复两个旧 fixture 断言（语义演进）；单测新增幻觉引导组；9 包 -race 全绿。I28 整体 done。

## 2026-11-11 A 切片：mcp client 错误路径直测（59.8%→63.1%）

- TestMain 扩展错误模式假 server（re-exec 同款零依赖，三 mode：err_list/empty/tool_err/err_exit）——4 组：JSON-RPC error 上抛（-32601）/空 content 空串无错/isError 转错误（磁盘已满）/连接中断（broken pipe 语义合理纳入）。
- 9 包 -race 全绿。

## 2026-11-11 C 切片：I11 步级断点续跑第 1/2 轮（结果重放数据层）

- runs.agent_version（执行时版本快照）+ run_steps.deterministic（tool 步）双列 additive（schema/mem/pg）。
- 重放通道：execute 内 RetryCount>0 → 取最近 run 确定性 tool 步（output.result 全量 2000 rune 新增落库——head 200 不够回灌）→ ExecRequest.ReplayTools → runner callTool 命中（ref+argsJSON，map 键序确定）返回已记录观察不重复调 MCP；tool 步 replay=true。**replay 端点清零重试 → 不带通道（完全重跑）**——两种重试语义边界清晰。
- 单测 3 组（命中/未命中走引导/无通道）；修一处反向断言自错。9 包 -race 全绿。第 2 轮：跨版本拒绝 + 幂等验收 + Spill 组合。

## 2026-11-11 A 切片：SSE 实时流直测（serveSSE/globalEvents/runEvents 0%→覆盖）

- 3 组：全局流（历史回放 seq1 + retry:3000 首连 + 实时推送 + id:/data: 格式 + 客户端断开退出）/since 过滤（since=1 不回放 seq1）/runEvents 404。
- **-race 抓到测试自身竞争**：主 goroutine 轮询 w.Body 与 SSE goroutine 并发写 httptest.Recorder——safeRecorder（mutex 包装 Write/Flush/Header/WriteHeader）修复。教训：python 多段替换目标串过时静默不命中（连续三次）——大段替换后必须 grep 验证。
- 9 包 -race 全绿。

## 2026-11-11 C 切片：I11 步级续跑第 2/2 轮（整体收口）

- 跨版本续跑拒绝：重放通道前置校验 lastRun.AgentVersion vs 当前链最大版本——不等 → task.replay_rejected 事件 + 完全重跑（旧观察不可信）；显式 max 版本消除 mem/pg 排序差异（pg DESC / mem 降序，vers[len-1] 都是坑）。
- 调度器级 e2e 3 组（captureRunner 注入全局注册表）：版本一致携带重放 / 版本变化拒绝+事件 / RetryCount=0 无通道。
- **踩坑**：重放段最初放 CreateRun 之后——runs[0] 是本次新建空 run，重放永远为空；移到 CreateRun 前修复（数据可见性时序）。I11 整体 done：Temporal 结果重放语义完整落地，重试不重复调 MCP。

## 2026-11-11 B 调研：Claude Code headless 驱动机制（docs/research/claude-code-headless-runner.md）

- 三输出协议（text/json/stream-json+result 末行）/会话续跑（continue/resume+SIGTERM 丢 turn 教训）/权限档位（auto/dontAsk/acceptEdits→映射 I29 风险分级）/事件类型（system.init capabilities、api_retry、parent_tool_use_id 子代理）/治理（--bare 可复现、--max-budget-usd、SIGINT 优雅中断）。
- **对 agentfleet**：I34 ClaudeCodeRunner 入池（真 coding-agent runner 执行体，I16 前置）；session 级续跑与 I11 步级重放互补分层确认；parent_tool_use_id↔parent_run_id 同构确认；SIGINT/SIGTERM 信号选择教训。
- 明确不抄：权限分类器/插件发现/OAuth 链。

## 2026-11-11 C 切片：I16 Workspace Runtime 第 1/2 轮（接口+状态机）

- internal/runtime 包：Runtime 接口（Probe→provisioning/ready/degraded 三态，与归档语义正交）+ Mock 恒就绪 + Local（目录存在 + git 分支匹配，3s 超时）；workspaces.runtime_status/runtime_note additive 双列。
- 创建即探测落库 + GET /workspaces/{id}/health 实时探测端点——workspace 从元数据绑定升级为可校验执行环境。
- 单测 4 组；修一处静默替换未命中（mem UpdateWorkspace 实际在 assets_mem.go——python 替换前先 grep 实现位置，教训再验证）。9 包 -race 全绿。第 2 轮：Docker 实现 + 前端徽章 + EnsureReady 接线。

## 2026-11-11 A 切片：Multica 系列 + 会话回放 HTTP 回归锁 + 抓到 Steer 真 bug

- Projects 生命周期（缺 name 400/创建/列表计数/删除）/Comments（@agent 唤醒回帖+线程）/Steer（非 running 409+running 落库+评论留痕）/conversations 列表+详情全量消息——此前仅手工实弹，handler 0% 补齐。
- **抓到两个真缺陷**：①mem UpdateTask 无 SteerNote 分支——Steer 功能在 --mem 模式（演示/测试主路径）写不进，决策边界永远拿不到插话（手工实弹只验了 200 没验注入）已修；②ListTaskComments map 遍历无序——线程时序漂移，加 ID 升序。
- 9 包 -race 全绿。

## 2026-11-11 C 切片：I16 Workspace Runtime 第 2/2 轮（整体收口）

- DockerRuntime（root_path 约定存容器名，docker inspect 校验 running）+ EnsureReady 接线：execute 绑定 workspace 时探测 degraded → failTask 拒绝执行不烧算力（错误带探测原因；预算内重试保留——环境可能恢复）+ /studio 状态徽章三色。
- 单测新增 3 组（Docker/拒绝/ready 对照）；9 包 -race 全绿。I16 整体 done——I34 ClaudeCodeRunner 前置就绪。

## 2026-11-11 A 切片：三大智能端点 HTTP 回归锁（F16/F17/F18-c）+ 修 prescribe panic

- precheck（409/503/假 Ollama 200 + ai-precheck gate 步）/compareEvals（400/404/improvement+delta）/prescribe（503/200 + 处方写回 report.BasedOnEval）——三大智能端点此前仅手工实弹，0% 补齐。
- gateway.NewWithBaseURL 导出构造器（依赖注入用例，additive）。
- **抓到真 panic**：prescribe 的 passes.String()[:400] 按字节截断——通过 case 短文本时越界 panic（且字节截断切中文 rune）；改 clip rune 安全截断。真模型长报告时不会触发（通过 case 多），短报告一调就炸——测试价值实证。
- 9 包 -race 全绿。

## 2026-11-11 C 切片：I34 ClaudeCodeRunner 第 1/2 轮（协议层）

- ClaudeCodeRunner.Execute：exec `claude --bare -p --output-format stream-json --verbose`（Workspace.RootPath 作 cwd——I16 落点）+ ccEvent 逐行容错解析：init→cc-init 门步 / assistant text→llm 步 / tool_use→tool 步 / result 末行=权威 Reply；Note 带 session_id + total_cost_usd。
- 取消语义对齐调研教训：cmd.Cancel 走 SIGINT（SIGTERM=143 丢 turn）+ 10min 超时 + ErrCanceled 传播（I1 对齐）。
- 测试：fake 脚本（t.TempDir 可执行文件替代真 claude 二进制）端到端 + 非零退出 cc-failed + 事件抽取锁 3 组；9 包 -race 全绿。第 2 轮：注册 fleetd + session 续跑 + permission 映射 + cost 接 F10。

## 2026-11-11 A 切片：深度功能面 handler 回归锁（F17-b/F18-c/I30）

- applyPrescription（无处方 409 / prompt 追加 addendum + 版本链发布——自进化闭环另一半）/routingStats（10 任务 2 重试 → escalation_rate_pct 20 healthy）/squads CRUD（建队 leader 校验/列表/详情/成员增删/删除）——0% 补齐。
- 9 包 -race 全绿。

## 2026-11-11 C 切片：I34 ClaudeCodeRunner 第 2/2 轮（整体收口）

- runs.session_id additive + store.SetRunSession（mem/pg）+ Result.SessionID/CostUSD 透传落库；重试 ResumeSession 携带（版本变化与重放同 gate 拒绝——调度器 e2e 两场景）；risk→permission-mode 映射（high→dontAsk / 其余 acceptEdits）；fleetd --claude-bin 注册（空=不注册不影响演示）。
- 参数构造纯逻辑锁 + e2e 2 场景；9 包 -race 全绿。**I34 整体 done：平台驱动外部 coding-agent 进程四面齐（协议/续跑/权限/成本）**。

## 2026-11-11 B 调研：LiteLLM Router（docs/research/litellm-router-gateway.md）

- 路由策略谱系（simple-shuffle 加权/usage-based 官方自认不适合生产/latency-based buffer 防单点/least-busy/cost-based）+ cooldown 机制（allowed_fails 计数、429 立即冷却、部署级粒度、到期回轮换）+ fallback 层级（order 逐层/错误类型分路径/重试责任单一所有者——SDK max_retries 钉 0 防叠乘）+ 会话粘滞。
- **对 agentfleet**：I35 gateway 模型级冷却入池（补「运行中模型持续故障反复撞」缺口）；重试责任单一所有者反证现架构正确；cost-based 哨兵价 vs 我们恒 0 的权衡记录。
- 明确不抄：Redis 跨实例/usage-based 路由/Routing Groups 语法糖。

## 2026-11-11 C 切片：I35 gateway 模型级冷却（LiteLLM cooldown 落地）

- coolingState（失败计数+冷却到期，自带锁——WithModel 值拷贝共享指针；修一个「锁拷贝」vet 报警：mu 不能随值拷贝，锁必须随状态指针走）；native/ChatRaw 错误→recordFailure（3 连败→冷却 60s）/成功→recordSuccess。
- SelectFor S 档冷却过滤回退 M 档；route 步 input.cooling_models 观测留痕。
- 单测 2 组（生命周期/SelectFor 命中→回退）；9 包 -race 全绿。S 档模型 429 不再反复撞。

## 2026-11-11 A 切片：CRUD 尾巴端点回归锁（api 包 0% 清零冲刺）

- listAgents（items/total）/workspaces 三件（list/get/patch branch+note 生效）/listAuthKeys/listAutomations/squad 成员移除（200+removed 标 / 404）——api 包 handler 面 0% 基本清零（仅剩 SetRateLimit 等 env 路径）。
- 9 包 -race 全绿。

## 2026-11-11 C 切片：I7 出站 Webhook 第 1/2 轮（投递核心）

- internal/webhook 包：Matches 通配过滤 / Sign HMAC-SHA256 / Deliver 三签名头投递（X-Fleet-Signature/Event/Timestamp）；webhooks 表（mem/pg）+ CRUD API（secret 明文仅创建响应，对齐 API Key 惯例）。
- 单测 4 组：签名确定性与接收方重算 / 事件过滤静默跳过 / 5xx 报错 / CRUD 全生命周期。9 包 -race 全绿。第 2 轮：fleetd dispatcher + 重试退避。
