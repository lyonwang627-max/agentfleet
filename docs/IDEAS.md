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
- [x] **I3 Agent 克隆** `done`（2026-11-07，后端）
  实现：POST /agents/{id}/clone——配置全复制（含 token 预算）、名称自动去重（-clone/-clone-2）、
  全新版本链 v1（不继承历史，回溯看源 agent）、克隆体默认启用。单测：配置复制/命名去重/独立性（改克隆不污染源）/版本链新起。前端克隆按钮待 F 系列后续切片。
- [x] **I4 评测数据集导入导出** `done`（2026-11-07）
  实现：GET /eval-datasets/{id}/export（attachment JSON）+ POST /eval-datasets/import（空 cases 400）；roundtrip 内容保真单测（points/threshold 全保真）。
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

- [ ] **I16 Workspace Runtime 接口** `proposed` `M`（源自 openhands-runtime-sandbox 调研，见 docs/research/）
  动机：现在 workspace 只是路径+分支的元数据绑定，agent 并不真在其中执行；借鉴 OpenHands 的 Runtime 抽象——
  「在 workspace 中执行」抽成接口（Mock/Local/Docker 三实现），workspace 加 provisioning/ready/degraded 状态机与健康检查。
  验收：接口定义 + 三实现迁移 + 现有测试全绿；DockerRuntime 复用 docker-compose 基建。接真 coding-agent runner 的前置。

- [ ] **I17 trace 层级化** `proposed` `M`（源自 dify-nested-tracing 调研，见 docs/research/）
  动机：run_steps 是平铺列表，handoff 链与多轮循环的嵌套关系只藏在 note 文本；借鉴 Dify 1.19.0 嵌套追踪——
  run_steps 加 parent_seq（循环观察步挂 decide 步下）+ 新 run 携 parent_run_id（handoff 链成树），瀑布图缩进渲染 + 跨 run 跳转。
  验收：handoff 后新 run 首步带 parent_run_id；瀑布图缩进展示嵌套；与 I15/F12 共用层级数据。

- [ ] **I18 死信队列与手动重放** `proposed` `M`（生产深度，对标 MQ 死信语义）
  动机：failed 任务与 done 混在列表里，重试语义模糊。failed 超过 max_retries 的任务进死信区（独立列表），支持查看失败上下文（最后 run 的错误与 trace 摘要）与一键重放（清 retry 计数重排队）。
  验收：GET /tasks?dlq=true 死信列表；POST /tasks/{id}/replay 重置重试计数；集成测试覆盖「重试耗尽→死信→重放→成功」。
- [ ] **I19 API Key 认证中间件** `proposed` `M`（生产深度，补全 F8 限流键升级路径）
  动机：目前无认证（内网演示定位），但向一线产品看齐必须有鉴权层。X-API-Key 中间件 + api_keys 表（key 哈希/scopes/最近使用）+ 限流键从 IP 换 key。
  验收：api_keys CRUD + 中间件校验（无 key 401/错 key 403/按 scope 放行）；--auth flag 开关默认关闭；限流键升级接线。
- [ ] **I20 任务依赖 DAG 可视化** `proposed` `S`（实用性）
  动机：depends_on 只有文字列表。任务详情页渲染依赖树（SVG 节点连线，标状态色），环检测在创建时报警。
  验收：GET /tasks/{id}/dag 返回节点/边；前端 SVG 渲染；创建时环检测 400。
- [ ] **I21 Prompt 模板变量** `proposed` `S`（技术深度）
  动机：system_prompt 是死文本。支持 {{task.title}}/{{workspace.path}} 等模板变量，执行时渲染并记录渲染后 prompt 落 trace（可审计）。
  验收：渲染函数 + 单测；trace 的 route 步带 rendered_prompt 摘要。
- [ ] **I22 事件流过滤查询** `proposed` `S`（实用性）
  动机：events 表已有全局 seq 但无查询端点。GET /events?kind=&task_id=&since=&limit= 服务端过滤，前端事件流可按类型/任务聚焦。
  验收：端点 + 单测；前端事件流加过滤器。
- [ ] **I23 运行报告 Markdown 导出** `proposed` `S`（实用性）
  动机：run trace 是 JSON 视图；导出为可读 Markdown 报告（步骤表格+耗时+结果），归档/分享用。
  验收：GET /runs/{id}/export.md；前端下载按钮。
- [ ] **I24 并发闸占用超时告警** `proposed` `S`（技术深度）
  动机：runner 卡死会占死并发闸（semaphore 无超时）。闸获取带超时并告警事件（与 F14 看门狗互补——看门狗看任务，这个看调度器自身）。
  验收：闸获取超时告警事件 + 单测。

## 已完成（沉淀记录）

- ~~任务级模型路由~~（F18-b，ADR-0002）
- ~~上下文 Spill~~（F11-b）
- ~~进展看门狗~~（F14）、~~AI 预审~~（F16）、~~RSI 处方闭环~~（F17）
- ~~限流~~（F8）、~~成本核算~~（F10）、~~优雅停机~~、~~单二进制交付~~
