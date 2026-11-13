# IDEAS — 自驱优化需求池

> **用法（给迭代 agent）**：本文件是项目自己的产品待办池。每轮「自提需求」切片：
> ①从池里挑一个 `proposed` 项（或基于对开源项目的调研新增一项，写清动机与对标）；
> ②实现 + 测试；③状态流转 proposed → in-progress → done（完成后同步 ROADMAP 若相关）。
> 提需求的纪律：每条必须有「动机（什么场景疼）」+「验收标准」+「量级估计（S/M/L）」。

## 池

- [x] **I1 任务取消传播到在途 run** `done`（2026-11-08，commit a15f77c + 323a212 状态补登 2026-11-10）
  实现：ExecRequest.CancelCheck（调度器注入 DB 二次确认）+ llm/mock runner 步边界协作退出 + ErrCanceled 专门分支（不重试不告警）；runner 单元级 + 调度器级 e2e（canceled 终态 200ms 翻转窗口确认）。方案源自 celery-revoke 调研。
- [x] **I2 Prometheus /metrics 端点** `done`（2026-11-07）
  实现：GET /metrics 手写 exposition（零 client 依赖）——tasks_by_status/tasks_total/agents_enabled/tokens_total{purpose,model}/rate_limit_rejected_total/eval_runs_total/uptime_seconds，HELP+TYPE 头按指标名去重；单测锁死关键行。压测对照可用。
- [x] **I3 Agent 克隆** `done`（2026-11-07，后端）
  实现：POST /agents/{id}/clone——配置全复制（含 token 预算）、名称自动去重（-clone/-clone-2）、
  全新版本链 v1（不继承历史，回溯看源 agent）、克隆体默认启用。单测：配置复制/命名去重/独立性（改克隆不污染源）/版本链新起。前端克隆按钮待 F 系列后续切片。
- [x] **I4 评测数据集导入导出** `done`（2026-11-07）
  实现：GET /eval-datasets/{id}/export（attachment JSON）+ POST /eval-datasets/import（空 cases 400）；roundtrip 内容保真单测（points/threshold 全保真）。
- [x] **I5 OpenAPI 规范固化** `done`（2026-11-07 落在 offer-agent——FastAPI 自动生成 OpenAPI 可导出固化+CI 漂移校验；agentfleet 为 Go 标准库 mux 无自动 OpenAPI，契约=手写 api-contract.md 天然固化，无需此机制）
- [x] **I6 chat 多轮上下文压缩** `done`（2026-11-10）
  实现：chatHistoryTranscript 纯函数（阈值 20 条/保留 12 条全文；较早消息确定性折叠为 40 rune 单行摘要，非模型摘要——同 F11 Spill 原则）；**三处接线**：sync/stream chat 补上多轮历史上下文（此前每轮只发当前消息，模型无记忆——比动机描述的「全量塞给模型」更根本的缺陷）+ 群聊 buildRoomPrompt 从全量无界改为有界时间线（roomKeepRecent=16）。单测 6 组：未压缩不误伤/压缩结构与折叠数/长度有界（60000→<6000 rune）/空输入/sync+stream 多轮记忆实弹（mock 回显）/room 压缩。验收达成：>20 轮仍可续聊且 prompt 有界。
- [x] **I7 事件订阅出站 Webhook** `done`（2026-11-11 + 第 3 轮幂等头 2026-11-13，1/2+2/2+3/3）
  动机：FDE 集成叙事——任务 done/failed 时回调客户系统 URL（带签名重试），对接企业现有告警/工单。
  验收：workspace 级 webhook 配置 + HMAC 签名 + 失败重试退避；契约新增。
  第 1 轮：internal/webhook 包（Matches 通配过滤 / Sign HMAC-SHA256 / Deliver 签名头投递——
  X-Fleet-Signature/Event/Timestamp 三头）+ webhooks 表（mem/pg）+ CRUD API（secret 明文仅创建响应，
  对齐 API Key 惯例）。单测 4 组（签名确定性与接收方重算/事件过滤静默/5xx 报错/CRUD）。
  第 2 轮：Dispatcher（轮询 EventsSince 增量拉取，重启从最新 seq 起——at-most-once 防重启风暴；
指数退避 1s/2s/4s/8s 上限 30s、attempt≤5 耗尽放弃；last_status/last_delivery_at 观测落库）+
fleetd 接线（go dispatcher.Run，无订阅空转零成本）。**抓到两个并发真 bug**：①scanOnce 持 d.mu 调
dispatch（内部再取锁）——Mutex 不可重入死锁，锁纪律改「d.mu 只护状态读写，网络 IO 绝不持锁」；
②重试到期项移出队列后 dispatch 重建条目 attempt 重置——退避永不耗尽，改 attempt 随调用透传只增不减。
单测 3 组（增量投递不重复/退避恢复 last_status=ok/耗尽放弃 http 500 留痕），webhook 包转正 10 包全绿。I7 整体 done。
- [x] **I8 Runner 工具池统一** `done`（2026-11-12，1/2 接口+双实现 + 2/2 runner 切换，M 之 1/2+2/2）
- [x] **I9 前端主题切换（暗/亮）** `done`（2026-11-10，commit 161ed18——状态补登）
  动机：演示时投影仪场景亮色更清晰；设计 token 已集中在 index.css，改造成本低。
  验收：顶栏切换按钮 + localStorage 记忆；两套 token 全量覆盖无硬编码色残留。
- [x] **I10 调度可插拔策略** `done`（2026-11-13，第 1/2 轮即完整落地，池里最后一个大项）
  动机：现在认领顺序固定 priority+id；把「选哪个任务」抽成 Strategy 接口（默认/fairness/deadline），为不同业务形态留扩展点。
  验收：接口 + 默认实现迁移 + 策略单测；行为不回归。
  实现：ClaimStrategy func(candidates []*model.Task) *model.Task（策略只选候选，闸门仍由存储统一保证）+
  策略注册表（priority/fairshare，RegisterClaimStrategy 可扩展）+ mem 候选集改造（闸门收集→策略选择）+
  pg ORDER BY 策略映射（priority→priority DESC,id / fairshare→priority DESC,created_at,id）+
  NewMemStoreWithStrategy / NewPostgresStore(dsn, strategy) / fleetd FLEET_CLAIM_STRATEGY env 接线。
  单测 3 组（默认不回归/fairshare created_at 最早/未知回退）。**踩坑**：候选值拷贝丢失指针更新致任务卡
  queued——改指针切片；mem/pg 两处 python 半改静默不命中（GetRun 列数 11/13）——grep 验证教训再证。
  第 2 轮（可选演进）：deadline 策略/策略热切换/策略命中观测指标。I10 done——IDEAS 池全清零。
  （I10 补充设计依据：kafka-consumer-rebalance 调研确认多实例无需 rebalance 协议——SKIP LOCKED
  天然增量，F13-b 只需 LISTEN/NOTIFY 唤醒。）

- [x] **I11 run 步级断点续跑** `done`（2026-11-11，1/2 结果重放 + 2/2 跨版本拒绝与 e2e，源自 langgraph-checkpointer + temporal-replay 两篇调研，见 docs/research/）
  动机：现在 run 失败只能整任务重试；借鉴 LangGraph checkpoint 恢复 + Temporal 结果重放语义——
  run 步骤标注确定性/非确定性：**确定性步（tool 调用）重试时返回已记录观察（Temporal 语义：结果重放而非跳过）**，非确定性步（llm）重新执行；trace 标注 resumed_from。
  验收：含 2+ tool 步的任务失败重试后，tool 步观察来自已记录结果（不重复调用 MCP）；跨 agent 版本（F2 version）变化的续跑请求被拒绝并提示重新评估；与 F11 Spill 组合。
  2026-11-10 langgraph-interrupt 调研补强验收：恢复=重放 → tool 步副作用须幂等（upsert 语义），与 Temporal「结果重放」双源互证；resume 值应结构化（非纯文本）。见 docs/research/langgraph-interrupt-resume.md。
  第 1 轮（结果重放数据层）：runs.agent_version（执行时版本快照——跨版本续跑拒绝的数据依据）+ run_steps.deterministic（tool 步=true）双列 additive；重放通道=execute 内 RetryCount>0 时取最近 run 的确定性 tool 步（output.result 全量 2000 rune 落库）构造 ExecRequest.ReplayTools → runner callTool 命中（ref+argsJSON 相同，json.Marshal map 键序确定）直接返回已记录观察不重复调 MCP；tool 步标 replay=true（Temporal 结果重放语义）。**语义边界：replay 端点清零重试 → 不带重放通道（完全重跑）**。单测 3 组：命中重放/未命中走引导/无通道真实路径。剩余（第 2 轮）：跨 agent 版本续跑拒绝 + 副作用幂等验收 + 与 F11 Spill 组合 e2e。

- [x] **I13 任务状态漂移检测器** `done`（2026-11-07，源自 openhands-event-stream 调研）
  实现：GET /tasks/{id}/consistency——replayTaskStatus 纯函数折叠 task.update 事件（终态屏蔽后续/审批回退合法路径）派生应有状态 vs tasks.status；人为制造漂移检出 ✓ 正常零误报 ✓。测试技巧：直调 handler 时需 r.SetPathValue 注入路径参数（绕过 mux 匹配，PathValue 才有值）

- [x] **I14 draft/published 分离** `done`（2026-11-10，1/2 后端 + 2/2 前端，源自 dify-draft-publish 调研，见 docs/research/）
  动机：现在 PATCH /agents/{id} 直接改运行时配置（编辑即上线，影响在途 run）；Dify 的 draft/published 分离让编辑不破坏生产。
  验收：agents.draft_config 列 + PATCH 只改 draft + POST /publish 快照生效 + draft hash 驱动「未发布变更」徽章 + rollback 先落 draft 可二次编辑（比现在多一档保守度）。
  第 1 轮（后端）：agents.draft_config JSONB（additive）+ store 三方法（UpdateAgentDraft 草稿合并/首次从 live 快照初始化、PublishAgentDraft 落线+清草稿、ClearAgentDraft）mem/pg 双实现 + PATCH 只进草稿（在途 run 免疫）+ publish=落线后版本快照（无草稿=纯快照，F2 语义保留）+ getAgent 带 draft_config/has_unpublished_changes + rollback 覆盖 live 后清草稿。单测 4 组：PATCH 不落线/publish 落线+版本链/无草稿纯快照/rollback 清草稿。剩余（第 2 轮）：前端「未发布变更」徽章 + 草稿编辑/发布 UX；rollback 只落 draft 的更保守语义评估。

- [x] **I15 eino 回调桥接** `done`（2026-11-07）
  实现：TraceHandler（callbacks.Handler + TimingChecker，五时点签名含 StreamReader[CallbackOutput] 流副本必须 Close 的契约）→ fleetd run_steps；ctx 双通道（InitCallbacks 注入 + TraceSinkKey 传 sink）；Generate 触发 OnStart/OnEnd/OnError 切面；EinoRunner 自动挂 sink。单测：三时点断言 + Generate 全链路（-tags eino）。

- [x] **I16 Workspace Runtime 接口** `done`（2026-11-11，1/2 接口+状态机 + 2/2 Docker/EnsureReady/徽章，源自 openhands-runtime-sandbox 调研，见 docs/research/）
  动机：现在 workspace 只是路径+分支的元数据绑定，agent 并不真在其中执行；借鉴 OpenHands 的 Runtime 抽象——
  「在 workspace 中执行」抽成接口（Mock/Local/Docker 三实现），workspace 加 provisioning/ready/degraded 状态机与健康检查。
  验收：接口定义 + 三实现迁移 + 现有测试全绿；DockerRuntime 复用 docker-compose 基建。接真 coding-agent runner 的前置。
  第 1 轮：internal/runtime 包（Runtime 接口 Probe→三态 provisioning/ready/degraded + Mock 恒就绪 + Local 目录存在/git 分支匹配校验）+ workspaces.runtime_status/runtime_note 两列 additive（与归档语义正交）+ 创建即探测落库 + GET /workspaces/{id}/health 实时探测端点。单测 4 组（Local 三分支/health 落库/创建即探测/mock）。剩余（第 2 轮）：DockerRuntime（docker exec 探测）+ 前端状态徽章 + runner 执行前 EnsureReady 接线 + I34 ClaudeCodeRunner 执行体。

- [x] **I17 trace 层级化** `done`（2026-11-10，1/2 数据层 + 2/2 前端，源自 dify-nested-tracing 调研，见 docs/research/）
  动机：run_steps 是平铺列表，handoff 链与多轮循环的嵌套关系只藏在 note 文本；借鉴 Dify 1.19.0 嵌套追踪——
  run_steps 加 parent_seq（循环观察步挂 decide 步下）+ 新 run 携 parent_run_id（handoff 链成树），瀑布图缩进渲染 + 跨 run 跳转。
  验收：handoff 后新 run 首步带 parent_run_id；瀑布图缩进展示嵌套；与 I15/F12 共用层级数据。
  第 1 轮（数据层）：run_steps.parent_seq + runs.parent_run_id 两列 additive（schema/mem/pg 双实现，pg AppendStep/ListSteps/GetRun/ListRuns scan 扩展）；LLM runner 发射改造——emit 返回落库步（AppendStep 回填 Seq），工具循环每轮先落 decide-turn-N 步、tool 观察步挂其下（parent_seq），汇总 decide 顶层；handoff 链：同任务第 2+ 次 run.ParentRunID 指向前一 run。单测 2 组（嵌套/平铺）。剩余（第 2 轮）：前端瀑布图缩进渲染 + 跨 run 跳转。

- [x] **I18 死信队列与手动重放** `done`（2026-11-07 后端）
  实现：GET /tasks?dlq=true（failed=重试耗尽语义的独立列表）+ POST /tasks/{id}/replay（清零重试预算重排队——与 retry 的区别是「人工判定值得从头再跑」而非「再试一次」）。单测：死信列表过滤/重放清零/非 failed 409 与 404。剩余：前端死信区展示。
- [x] **I19 API Key 认证中间件** `done`（2026-11-07）
  实现：api_keys 表（SHA-256 哈希库存，明文仅创建响应一次）+ X-API-Key 中间件（渐进语义：表空开放→有 key 强制）+ 管理端点三件（POST 创建返回明文/GET 列表/DELETE 撤销）+ 限流键升级接线（认证后按 key 名限流）+ metrics/health 豁免。单测：七步渐进语义全链（开放→建 key→401→403→200→豁免→撤销恢复）。
- [x] **I20 任务依赖 DAG 可视化** `done`（2026-11-10，后端 c4e3a6e + 前端 1d5bef4）
  实现：detectCycle DFS 三色 + validateNoCycle（防御深度；创建时序决定传递环不可达——deps 只能引用已存在任务）+
  GET /tasks/{id}/dag（子图节点/边 + cycle 真算）+ createTask 缺失依赖 400（防永久 blocked 卡死）+ deps 去重 +
  TaskPatch.DependsOn（mem/pg 双实现）+ 前端 DepGraph SVG（最长路径分层/贝塞尔连线/状态着色/像素预算截断零溢出/红环警示）。
  顺手修 TaskDetail 钩子违规崩溃（useEffect 在早退 return 后）。回归锁：算法单测 + HTTP 级 + 浏览器 getComputedTextLength 实测。
- [x] **I21 Prompt 模板变量** `done`（2026-11-07）
  实现：renderTemplate 纯函数（task.title/task.id/workspace.path/branch/name/agent.name 六变量；未知变量原样保留——写错应被看见；无模板快路径零开销；nil 安全）；渲染只发生在执行副本（不污染任务本体）。单测：全渲染/未知保留/快路径/nil 安全/agent.name 五组。trace 审计：渲染发生在 route 步之后的执行链路，task 本体保存未渲染原文。
- [x] **I22 事件流过滤查询** `done`（2026-11-07）
  实现：GET /events/query?kind=&task_id=&since=&limit=（服务端过滤，data 内嵌 task_id 也能过滤）+ 单测三场景（kind/task_id/limit）。前端事件流加过滤器待后续前端切片。
- [x] **I23 运行报告 Markdown 导出** `done`（2026-11-07，commit 7053cac——状态补登）
  动机：run trace 是 JSON 视图；导出为可读 Markdown 报告（步骤表格+耗时+结果），归档/分享用。
  验收：GET /runs/{id}/export.md；前端下载按钮。
- [x] **I24 并发闸占用超时告警** `done`（2026-11-07）
  实现：闸获取 select 加超时分支——超时任务延迟重排队（非 failed，拥塞不是错误）+ agent.gate_timeout 事件。**架构认知（测试确立）**：ClaimNextTask 已在认领时强制并发检查，闸超时只覆盖「认领与执行之间的竞态窗口」——卡死占闸的主场景由看门狗（运行中任务）与依赖级联（queued 任务）治理。单测：手动占闸 + 直调 execute 精确命中竞态窗口。

- [x] **I28 步骤级 LLM 路由** `done`（2026-11-10，1/2 能力清单 + 2/2 幻觉引导强约束，源自 eino-adk-supervisor 调研，见 docs/research/）
  动机：F11 多轮循环的工具选择是固定 JSON 协议；借鉴 eino ADK supervisor——decide 阶段基于「能力描述」由 LLM 选择委派对象（工具/子能力），supervisor 模式下沉到 run 内部。
  验收：工具池带描述元数据；decide 提示含能力清单；与现有 handoff/审批语义兼容。
  第 1 轮：ToolCatalog（动态发现 name+描述 60 rune 摘要，无 server 返回空降级）+ decide user 注入「可用工具」段（LLM 按能力描述选工具，supervisor 下沉）。单测 2 组（空清单/无注入）。**实弹发现（qwen3.5）**：清单注入后模型仍幻觉编造 server 名（请求 mcp:system/clock 而非 mcp:demo/now）——小模型对清单的遵循度有限，第 2 轮须加「禁止选清单外工具」强约束 + 幻觉工具名时 decide 重试引导（错误观察回灌已天然支持：CallMCPTool 未注册报错会转观察回下一轮）。

- [x] **I29 审批风险分级** `done`（2026-11-07）
  实现：Task/Result 加 RiskLevel（low/medium/high）；mock 按关键词定级（对外发布/删除/资金→high）；调度器 review 门带 risk 标注落 gate step 与事件；**high 任务 approve 无意见 400**（强制留痕）；precheck risk=high 写回 task.RiskLevel（F16 联动）。单测：high 无意见 400/带意见 200/medium 不强制。顺手修：Server.publish nil 守卫（测试直调 handler 不再 panic）+ 全部子文件 Hub 调用统一。

- [x] **I30 小队模式** `done`（2026-11-09，对标 Multica squads 源码调研定稿）
  实现：squad/squad_members 表与 CRUD（leader 必为 agent、成员混编、UNIQUE 幂等）+ leader briefing（运行协议：只协调不动手/转派/无动作记录/派完即停 + 花名册含技能 + 小队指令）注入 system prompt + 任务创建接受 squad_id（缺省 leader/非 leader 400）+ 成员完成自动生成 leader 跟进评估任务（事件驱动闭环）。端到端测试：建队→briefing 四要素→leader handoff→成员完成→跟进任务生成。

- [x] **I32 群聊候选集收窄（@all 智能选人）** `done`（2026-11-10）
  动机：room 的 @all 现在全员唤醒（token 浪费且常不必要）；AutoGen candidate_func 思想——
  LLM 只做「候选集内选人」这一步骤级自动环节：显式 @ 优先（可审计的权威通道不变），
  @all/未指定时由模型从 roster（I30 花名册已带技能描述，selector 数据已就位）选最相关 1-2 名。
  验收：@all 时模型选人输出带理由；显式 @ 行为完全不回归；选人失败回退全员唤醒。
  实现：selectRoomAgents（ChatJSON + roomSelectorSystem 选人器人格，roster=enabled 成员 name:role+提示词头 60，上下文=最近 6 条头部）→ ≤2 名有效选人（名单外幽灵名过滤、去重）；响应 additive 字段 selection（名字+理由）。单测 4 组：智能选人（ghost 过滤）/非法 JSON 回退全员/全无效名回退/显式 @ 不经选人器。真模型实弹（qwen3.5）：@all → 选中 intake 带理由 → intake 链式 @fixer 协作；幻觉 @ops（seed 无此名）被安全忽略。

- [x] **I33 工具步级审批（risk 下沉到工具调用粒度）** `done`（2026-11-10，1/2 LLM runner + 2/2 mock 剧本与拒绝留痕，源自 langgraph-interrupt 调研，见 docs/research/）
  动机：审批门现为任务级（review→approve→requeue 整任务重跑）；LangGraph 工具内 interrupt 证明审批可随工具走——
  high 风险任务（I29）的特定工具调用（对外发布/删除/资金类）执行前暂停，approve 带参数修正后执行，reject 跳过该工具继续。
  验收：mock 剧本高风险工具步进 review 门（带工具名/参数快照）；approve 带参数修正注入当步执行；reject 跳过并在 trace 留 gate-reject 步；F11 工具循环内同样生效。
  第 1 轮（LLM runner 生产路径）：highRiskTool 关键词判定（I29 同源：发布/删除/资金/部署）+ callTool 包装闸（ToolGateError 哨兵中止 runToolLoop——runToolLoop 语义扩展：callTool 可中止）→ Execute 映射 NeedsApproval/high + gate 步 tool-approval-gate 带工具/参数快照（不是失败，任务进 review 门）。放行契约：approve 审批意见必须提及工具名（toolApproved 防一次审批放行全部工具），F3 审批记录注入 task prompt 重跑时闸自然放行；参数修正经审批意见注入 prompt 由模型消费（与 F3 同机制）。单测 4 组：高风险拦截（gate 步+快照）/低风险回归/批准后放行（工具真实执行+观察回灌）/判定表。第 2 轮：mock 剧本工具路径评估 + reject（cancel）trace 留痕 gate-reject 步。

- [x] **I34 ClaudeCodeRunner（外部 agent 进程执行体）** `done`（2026-11-11，1/2 协议层 + 2/2 接线收口，源自 claude-code-headless 调研，见 docs/research/）
  动机：runner v1 是 mock/llm（对话型），接真 coding-agent 是平台能力跃迁——Claude Code `-p --bare`
  是最成熟的被驱动对象：stream-json 行解析（复用 F15 NDJSON 经验）+ session_id 落库（会话级续跑，
  与 I11 步级重放互补）+ total_cost_usd 直连 F10 成本 + permission-mode 映射 I29 风险分级。
  验收：ClaudeCodeRunner 实现 runner 接口；stream-json 事件转 run_steps（assistant→llm/tool 步映射）；
  result 末行取 Reply 与成本；session 续跑选项；SIGINT 优雅中断（SIGTERM=143 丢 turn 教训）。
  前置：I16 Local Runtime（工作目录绑定）。
  第 1 轮（协议层）：ClaudeCodeRunner.Execute——exec `--bare -p` + stream-json 逐行解析（ccEvent 容错）：
  system/init→cc-init 门步、assistant text→cc-assistant llm 步、tool_use→tool 步、result 末行=权威 Reply、
  Note 带 session_id/total_cost_usd；SIGINT 取消（cmd.Cancel 自定义）+ ccDefaultTimeout 10min +
  Workspace.RootPath 作 cwd（I16 落点）；ErrCanceled 传播对齐 I1。测试：fake 脚本（忽略参数输出预置 JSONL）
  端到端 + 非零退出 cc-failed + 事件抽取锁。剩余（第 2 轮）：runner 注册进 fleetd（runner=claude-code 槽位）
  + session_id 持久化续跑选项 + permission-mode 映射 I29 + cost 接 F10。

- [x] **I35 gateway 模型级冷却** `done`（2026-11-11，源自 litellm-router 调研，见 docs/research/）
  动机：gateway 现在启动时 Resolve 一次探测，「运行中模型持续 429/超时」会反复撞——
  LiteLLM cooldown 机制：连续 allowed_fails 次失败 → 冷却 cooldown_time 秒 →
  TierToModel 选模型时过滤冷却中模型（同档无备选回退 agent 自带，只慢不错）。
  验收：模型失败计数/冷却/到期回轮换（gateway 内存态单测）；TierToModel 过滤冷却模型；
  冷却期事件留痕（gateway.model_cooldown）。
  实现：coolingState（自带锁——WithModel 值拷贝共享指针，双视图并发安全，修一个锁拷贝 vet 报警）
  + native/ChatRaw 错误→recordFailure（3 次连续 → 冷却 60s）/成功→recordSuccess 重置 +
  SelectFor S 档冷却过滤（回退 M 档——语义为「回退」非「返回空」）+ route 步 input.cooling_models 留痕。
  单测 2 组（生命周期：3 败冷却/到期回轮换/成功重置；SelectFor 未冷却命中→冷却回退 M）。S 档 429 不再反复撞。

- [x] **I36 PG 真机验证（embedded-postgres 冒烟）** `done`（2026-11-13，源自「pg store 零真机验证」诚实缺口）
  实现：测试内嵌真 PG 17（fergusstrange/embedded-postgres，test-only 依赖不污染系统，凭据对齐 compose）+
  全链路冒烟 2 组（agent 草稿三方法/task patch/run 扩列往返/webhook jsonb/版本链/usage purpose）。
  **累计抓 7 个真 bug，全部修复**：①schema.sql 21 条 ALTER 前置于 CREATE——全新部署 migrate 必炸
  （conversations/messages/run_steps 等，旧库掩蔽）→ ALTER 统一后置；②③pg UpdateTask/ListRuns/GetRun
  各缺 SteerNote/session_id 等列（python 半改静默不命中的后果——scan 列数对齐系统性核查）；
  ④pg AppendStep 误透传 Seq=0 破坏主键递增 → DB 侧 max+1；⑤⑥CreateRun 只插 3 列丢
  parent_run_id/agent_version/session_id；⑦usage_events 缺 purpose 列（schema 漏 ALTER）。
  **教训：mem 双实现掩蔽了全部 SQL 缺陷——新列落地必须真机冒烟。**
  第二波扩面（squads/projects/comments/automations/skills/evals/api_keys 删除闭环）又抓 3 个真 bug：
  ⑧pg UpdateTask 缺 Labels 分支（mem/pg 双实现不齐第二例）；⑨pg DeleteSquad 未级联清
  squad_members——FK 约束下删队直接炸（mem 遍历删除掩盖）；⑩沿袭确认 hash 查找/UNIQUE 幂等正常。
  pg 真机冒烟累计 **10 个真 bug**——两波扩面后 store 接口面真机覆盖基本完整。

- [x] **I37 入站 webhook endpoint（外部系统→任务）** `done`（2026-11-13，源自 dify-plugin-daemon 调研，见 docs/research/）
  动机：I7 只有出站（结果通知），Dify Endpoint 类型补全双向集成——平台生成一次性 URL，
  外部告警/工单系统回调 → 校验映射 → 自动建任务（FDE 集成叙事闭环：告警系统进=入站、结果通知出=出站）。
  验收：POST /api/v1/webhooks/{id}/ingest/{token}——token 校验 → payload 映射 title/prompt → 建任务；
  无效 token 401；payload 缺字段 400；ingest 事件流留痕。
  实现：IngestToken（SHA256("ingest:"+secret) 派生 24 hex——secret 本体不暴露，token 可独立分发/轮换）+
  POST /webhooks/{id}/ingest/{token}（payload agent_id/title/prompt/priority/labels → 任务 + 来源标签
  webhook:<id>）+ webhook.ingest 事件留痕 + Wake 即时派发。单测五断言（401/400 缺字段/400 agent 无效/
  201 映射+标签+事件）。FDE 双向集成闭环：告警系统进（入站）= 结果通知出（出站 I7）。
  边界收尾：**Idempotency-Key 幂等**（外部系统重试同 key → 返回首个任务 deduplicated=true，
  10 分钟 TTL 内存去重；不同 key 各自建；无 key 调用方自担）——单测四场景。
  （I6 交叉引用：dify-conversation-memory 调研确认「零 LLM 成本确定性折叠」与业界取舍一致，
  summary/vector 均非 Dify 原生——详见 docs/research/dify-conversation-memory.md。）

- [x] **I38 agent 版本灰度发布（ramping 式确定性分桶）** `done`（2026-11-13，1/2 数据层+分桶 + 2/2 promote+前端，源自 temporal-worker-versioning 调研，见 docs/research/）
  动机：I14 publish 是全量立即切换；Temporal Ramping 按百分比灰度——发布新版本时按 task ID
  确定性分桶路由新任务（Pinned run 不受影响），平滑升级。
  验收：agents 表 ramping_version/ramp_pct 列；新任务按 task ID 确定性分桶（同任务判定一致）；
  ramp=0 全走 current；重试/续跑仍锁原版本（I11 语义不变）。
  第 1 轮：agents.ramping_version/ramp_pct 双列 additive（pg scan/patch + mem）+ routeVersion 纯函数
  （fnv32(taskID:rampingVersion) 确定性分桶——掺版本号使不同灰度轮分桶可变）+ applyVersionSnapshot
  （灰度桶用版本链 Config 快照覆盖 agent 执行副本 system_prompt/model）+ publishAgent ramp_pct 参数
  （>0 时 RampingVersion 落库 + 版本备注灰度意图）。单测 2 组（分桶确定性/边界 0 与 100/快照覆盖）。
  第 2 轮：**promote 语义**（灰度收敛——live 配置更新为 ramping 快照 + 清标记，版本链不膨胀；
  无灰度 409）+ 前端（灰度徽章「灰度 N% → vN」+ 发布旁「全量」按钮 + AgentVersions 状态条）。
  单测新增（ramp 落库/promote 收敛/无灰度 409）。I38 整体 done：Temporal Ramping 语义完整落地。
  **e2e 追加抓 1 个语义 bug**：ramp 发布时 PublishAgentDraft 落线草稿——current 桶灰度失效；
  修复=ramp>0 时草稿只进版本快照不落线（current 桶保持旧配置），promote 才落线；快照取草稿内容。
  调度器级 e2e（ramp=100 全新配置/ramp=0 全旧）+ API 层语义锁（ramp 不落线/promote 落线）全绿。

- [ ] **I39 events_query 相对时间语法** `proposed` `S`（源自 temporal-search-attributes 调研，见 docs/research/）
  动机：events_query 的 since 只收绝对 seq/time——Temporal 的 `NOW()-'24h'` 相对时间语法
  对运维查询更友好（昨天的失败任务：since=NOW()-24h&kind=task.failed）。
  验收：since 支持 `NOW()-<duration>` 形态（Go time.ParseDuration 子集：s/m/h/d）；
  绝对 seq/time 完全兼容（不回归）；单测覆盖相对时间解析与边界。

- [x] **I40 评测数据集版本化** `done`（2026-11-14，1/2 数据层 + 2/2 compare 对齐，源自 langsmith-evaluation 调研，见 docs/research/）
  动机：SaveCases 直接覆盖 cases——数据集变更多次评测间不可比（F18-c compare 按 case_id
  对齐，改动后旧 eval 对不上新 cases）；LangSmith 语义=example 变更自动产生新版本快照。
  验收：eval_datasets 加 version 列（SaveCases 时自增）+ eval_runs 记录评测时的 dataset_version
  + compare 时按版本对齐 case_id；旧数据兼容（version=0）。
  第 1 轮（数据层）：eval_datasets.version（SaveCases 自增/初始 1，mem/pg 双实现——pg 附带修
  SaveCases 全量替换语义补 DELETE）+ eval_runs.dataset_version（CreateEvalRun 时从 datasets
  子查询落库；GetEvalRun/ListEvalRuns 读路径扩展）+ EvalDataset.Version/EvalRun.DatasetVersion
  additive。测试：pg 真机冒烟（1→3 自增 + run 记录版本）+ mem 对齐组。剩余（第 2 轮）：compare
  按版本对齐提示 + 前端版本显示。

- [ ] **I41 slog 结构化日志统一** `in-progress`（1/2 scheduler 核心路径完成 2026-11-14，源自 structured-logging-slog-loki 调研，见 docs/research/）
  动机：fleetd 日志全是 log.Printf 文本——task_id/run_id/agent 嵌在消息里不可查询；
  slog 字段化（与 events 表字段形态对齐，日志与事件流互查）是生产化缺口。
  验收：Printf → slog 全量迁移（key-value：task_id/run_id/agent/model）；run 上下文 slog.With
  注入 run_id 根字段（轻量 trace 关联）；log 输出形态不破坏现有测试（可替换 handler）。
  第 1 轮：scheduler.go 16 条 Printf → slog 全量迁移（task dispatched/done/failed/retry/handoff/
  重放拒绝/闸超时/依赖级联/小队——task_id/run_id/agent/retry 为独立字段）+ main 侧 JSON handler
  待第 2 轮统一。单测：slog.SetDefault 替换 JSON handler（lockedBuffer 并发安全）断言 dispatch
  字段化（task_id/run_id 独立字段非消息文本）。剩余（第 2 轮）：main/hub/automations/gateway/runner
  全量迁移 + JSON handler 默认化。

- [x] **I42 ChatJSON schema 升级预留（json_object→strict 级）** `done`（2026-11-14，源自 structured-outputs 调研，见 docs/research/）
  动机：gateway `format:"json"` 是 json_object 级保证（语法合法/schema 不保证）；Ollama
  新版 format 接受 JSON Schema（grammar 约束——strict 级，schema 违反结构性不可能）。
  验收：ChatJSON 加可选 schema 参数（nil=现状零回归）→ payload.format 传 schema；
  decision 协议 schema 固化传入；无 schema 时回退 json_object。
  实现：native 加 schema 参数（nil="json" 字符串/有=schema 对象——strict 级判定）+
  ChatJSONSchema 新方法（ChatJSON 委托 nil 零回归）+ llm.go decisionSchema 固化（decision
  结构体六字段 schema）传入 decide 每轮。单测：payload.format 为 schema 对象断言。
  附带修 dispatcher timeNow 包级变量 race（测试互写）→ Dispatcher.timeNow 实例字段。I42 整体 done。

## 已完成（沉淀记录）

- ~~任务级模型路由~~（F18-b，ADR-0002）
- ~~上下文 Spill~~（F11-b）
- ~~进展看门狗~~（F14）、~~AI 预审~~（F16）、~~RSI 处方闭环~~（F17）
- ~~限流~~（F8）、~~成本核算~~（F10）、~~优雅停机~~、~~单二进制交付~~
