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
  2026-11-10 langgraph-interrupt 调研补强验收：恢复=重放 → tool 步副作用须幂等（upsert 语义），与 Temporal「结果重放」双源互证；resume 值应结构化（非纯文本）。见 docs/research/langgraph-interrupt-resume.md。

- [x] **I13 任务状态漂移检测器** `done`（2026-11-07，源自 openhands-event-stream 调研）
  实现：GET /tasks/{id}/consistency——replayTaskStatus 纯函数折叠 task.update 事件（终态屏蔽后续/审批回退合法路径）派生应有状态 vs tasks.status；人为制造漂移检出 ✓ 正常零误报 ✓。测试技巧：直调 handler 时需 r.SetPathValue 注入路径参数（绕过 mux 匹配，PathValue 才有值）

- [x] **I14 draft/published 分离** `done`（2026-11-10，1/2 后端 + 2/2 前端，源自 dify-draft-publish 调研，见 docs/research/）
  动机：现在 PATCH /agents/{id} 直接改运行时配置（编辑即上线，影响在途 run）；Dify 的 draft/published 分离让编辑不破坏生产。
  验收：agents.draft_config 列 + PATCH 只改 draft + POST /publish 快照生效 + draft hash 驱动「未发布变更」徽章 + rollback 先落 draft 可二次编辑（比现在多一档保守度）。
  第 1 轮（后端）：agents.draft_config JSONB（additive）+ store 三方法（UpdateAgentDraft 草稿合并/首次从 live 快照初始化、PublishAgentDraft 落线+清草稿、ClearAgentDraft）mem/pg 双实现 + PATCH 只进草稿（在途 run 免疫）+ publish=落线后版本快照（无草稿=纯快照，F2 语义保留）+ getAgent 带 draft_config/has_unpublished_changes + rollback 覆盖 live 后清草稿。单测 4 组：PATCH 不落线/publish 落线+版本链/无草稿纯快照/rollback 清草稿。剩余（第 2 轮）：前端「未发布变更」徽章 + 草稿编辑/发布 UX；rollback 只落 draft 的更保守语义评估。

- [x] **I15 eino 回调桥接** `done`（2026-11-07）
  实现：TraceHandler（callbacks.Handler + TimingChecker，五时点签名含 StreamReader[CallbackOutput] 流副本必须 Close 的契约）→ fleetd run_steps；ctx 双通道（InitCallbacks 注入 + TraceSinkKey 传 sink）；Generate 触发 OnStart/OnEnd/OnError 切面；EinoRunner 自动挂 sink。单测：三时点断言 + Generate 全链路（-tags eino）。

- [ ] **I16 Workspace Runtime 接口** `proposed` `M`（源自 openhands-runtime-sandbox 调研，见 docs/research/）
  动机：现在 workspace 只是路径+分支的元数据绑定，agent 并不真在其中执行；借鉴 OpenHands 的 Runtime 抽象——
  「在 workspace 中执行」抽成接口（Mock/Local/Docker 三实现），workspace 加 provisioning/ready/degraded 状态机与健康检查。
  验收：接口定义 + 三实现迁移 + 现有测试全绿；DockerRuntime 复用 docker-compose 基建。接真 coding-agent runner 的前置。

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
- [ ] **I23 运行报告 Markdown 导出** `proposed` `S`（实用性）
  动机：run trace 是 JSON 视图；导出为可读 Markdown 报告（步骤表格+耗时+结果），归档/分享用。
  验收：GET /runs/{id}/export.md；前端下载按钮。
- [x] **I24 并发闸占用超时告警** `done`（2026-11-07）
  实现：闸获取 select 加超时分支——超时任务延迟重排队（非 failed，拥塞不是错误）+ agent.gate_timeout 事件。**架构认知（测试确立）**：ClaimNextTask 已在认领时强制并发检查，闸超时只覆盖「认领与执行之间的竞态窗口」——卡死占闸的主场景由看门狗（运行中任务）与依赖级联（queued 任务）治理。单测：手动占闸 + 直调 execute 精确命中竞态窗口。

- [ ] **I28 步骤级 LLM 路由** `in-progress`（1/2 能力清单完成 2026-11-10，源自 eino-adk-supervisor 调研，见 docs/research/）
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

## 已完成（沉淀记录）

- ~~任务级模型路由~~（F18-b，ADR-0002）
- ~~上下文 Spill~~（F11-b）
- ~~进展看门狗~~（F14）、~~AI 预审~~（F16）、~~RSI 处方闭环~~（F17）
- ~~限流~~（F8）、~~成本核算~~（F10）、~~优雅停机~~、~~单二进制交付~~
