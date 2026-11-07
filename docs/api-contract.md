# agentfleet API Contract v1

Base URL: `http://localhost:8090/api/v1`（开发环境 Vite proxy `/api` → `localhost:8090`）
所有响应 JSON；错误统一 `{"error": {"code": "...", "message": "..."}}`，HTTP 状态码语义化。
分页：`?page=1&page_size=20` → `{"items": [...], "total": n, "page": 1, "page_size": 20}`（列表类接口默认带）。

## Health
### GET /health
`{"status":"ok","db":true,"scheduler":true,"llm":{"mode":"ollama|mock","model":"qwen3.5"},"uptime_s":123}`

## Agents（舰队）
### POST /agents
body `{"name":"coder","role":"后端开发","system_prompt":"你是…","model":"qwen3.5","tools":["http_get","text_search"],"max_concurrency":2,"runner":"llm|mock"}`
→ `{"id":1}`（runner: llm=真实模型, mock=确定性演示）
**v1.2 新增字段**：`token_budget_24h`（int，0=不限）——24h 用量达预算时调度器自动停用该 agent 并广播 SSE 事件 `agent.budget_exceeded`（data: agent_id/used_24h/budget/action=paused）；人工 enable 即恢复（恢复口径由人决定）。run 完成后其 tokens 自动回填 usage_events
### GET /agents → items: `{"id","name","role","model","runner","enabled","max_concurrency","running","queued","created_at"}`
### GET /agents/{id} → 上述 + `system_prompt","tools","stats":{"total_runs","success_rate","avg_latency_ms","tokens_30d"}`
### PATCH /agents/{id}（部分更新任意字段）→ `{"id":1}`
### POST /agents/{id}/enable | /agents/{id}/disable → `{"id":1,"enabled":true|false}`

## Tasks（任务系统）
### POST /tasks
body `{"title":"修复登录超时 bug","prompt":"详细任务说明…","agent_id":1,"priority":5,"depends_on":[3,7]}`
→ `{"id":101,"status":"queued"}`（priority 1-9，9 最高；depends_on 为前置任务 id）
### GET /tasks?status=queued|running|review|done|failed&agent_id= → items:
`{"id","title","status","priority","agent_id","agent_name","created_at","started_at","finished_at","blocked_by":[3]}`
### GET /tasks/{id} → 上述 + `prompt","result","error","deps":[{"id","title","status"}]`
### POST /tasks/{id}/retry → 重派（failed→queued，清空 error）
### POST /tasks/{id}/cancel → queued/running/review → canceled
### POST /tasks/{id}/approve  body 可选 `{"note":"审批意见"}`（v1.2 增强）
→ `{"id":1,"action":"approve"}`；意见注入任务 prompt（[审批记录] 段）并作为 gate 步骤落最近 run 的 trace
### GET /metrics（v1.4 新增，I2 Prometheus 文本格式）→ `text/plain; version=0.0.4`
指标：agentfleet_tasks_by_status / tasks_total / agents_enabled / tokens_total{purpose,model} / rate_limit_rejected_total / eval_runs_total / uptime_seconds

### GET /tasks/{id}/consistency（v1.4 新增，I13 状态漂移检测）→ `{"task_id","actual_status","replayed_status","events_seen","consistent","hint"?}`
从 events 表重放 task.update 事件派生应有状态并与实际比对——双写漂移即报警（调试端点）

### GET /tasks?dlq=true（v1.4 新增，I18 死信列表）→ 仅 failed（重试耗尽）任务
### POST /tasks/{id}/replay（v1.4 新增，I18 死信重放）→ `{"id","action":"replay"}`（清零重试预算重排队；仅 failed 可 replay，否则 409）

### POST /tasks/{id}/precheck（v1.3 新增，F16 AI 预审）→ `{"recommendation":"approve|deny|needs_human","risk_level":"low|medium|high","reason":"…"}`
仅 review 状态可用；无工具预审模型评估操作风险+产出是否回应诉求，**只给建议不执行**，人仍终审；结果落 trace（kind=gate, name=ai-precheck）+ SSE `task.precheck`。LLM 不可用时返回 503（人工直接 approve）

## Runs（执行与观测）
### GET /tasks/{id}/runs → `[{"id":1,"status":"running","started_at","finished_at","total_tokens","cost_usd"}]`
### GET /runs/{id} → trace 详情：
```json
{"id":1,"task_id":101,"agent_id":1,"status":"done","total_tokens":1850,"cost_usd":0,
 "steps":[{"seq":1,"kind":"llm","name":"plan","input":{...},"output":{...},"latency_ms":4200,"tokens":900},
          {"seq":2,"kind":"tool","name":"http_get","input":{...},"output":{...},"latency_ms":120},
          {"seq":3,"kind":"handoff","name":"->reviewer","note":"…","latency_ms":0}]}
```
（kind: llm / tool / handoff / gate）
### GET /runs/{id}/events → **SSE**，事件 `{"seq":n,"kind":"llm|tool|handoff|gate|done|error","data":{...}}`
### GET /events → 全局 SSE 事件流（任务/Run 状态变化，前端看板实时刷新用）

## Chat（交互式试用 Agent）
### POST /chat  body `{"agent_id":1,"message":"帮我看看这个任务怎么做","conversation_id":null}`
→ `{"conversation_id":9,"reply":"…","run_id":88}`（也会产生 run/trace，可回看）

## Gateway（用量）
### GET /usage?window=24h|7d → `{"window":"24h","requests":42,"prompt_tokens":184000,"completion_tokens":96000,"cost_usd":0,"cost_by_model":{...},"cost_by_agent":{...},"by_model":[...],"by_agent":[...],"rate_limit":{...}}`
**v1.4 新增字段（F10/F18-a）**：`cost_usd` / `cost_by_model` / `cost_by_agent`（定价表聚合）；`by_purpose`（用量按用途分桶：execute/chat/judge/review——模型路由可观测维度）；：按模型定价表聚合（$/1M tokens；本地 Ollama 模型=0，云端模型经 FLEET_PRICING env 覆写）；未知/本地模型成本恒 0（面板定位成本量级感知，非财务结算）
### GET /models → `{"models":[{"name":"qwen3.5","provider":"ollama","available":true},{"name":"mock","provider":"builtin","available":true}]}`

## Eval（评测）
### POST /evals  body `{"agent_id":1,"dataset_id":1}` → `{"id":7,"status":"running"}`
### GET /evals → `[{"id","dataset_id","agent_id","status","score_avg","created_at"}]`
### GET /evals/{id} → `{"id","status","report":{"cases":[{"case_id","score","pass","judge_comment"}],"score_avg","pass_rate"}}`
### GET /eval-datasets → `[{"id","name","description","cases":12}]`

### GET /mcp/servers（v1.2 新增，F4）→ `{"servers":[{"name":"demo","tools":[{"name","description","inputSchema"}]}]}`
（MCP stdio server 注册表：启动参数 `--mcp demo=auto`；工具列表从 server 动态发现。任务 prompt 中 `MCP:server/tool 参数` 指令会真实调用 MCP server 并在 trace 落 tool 步）

## Agent 版本管理（v1.2 新增，F2）
### POST /agents/{id}/publish  body `{"note":"首次发布"}`（note 可选）
→ `{"agent_id":1,"version":1}`（快照当前配置；版本链只前进）
### GET /agents/{id}/versions → `{"items":[{"id","agent_id","version","config":{name,role,system_prompt,model,tools,runner,max_concurrency},"note","created_at"}],"total":n}`（最新在前）
### POST /agents/{id}/versions/{version}/rollback → `{"agent_id":1,"rolled_back_from":1,"new_version":3}`
（旧快照覆盖当前配置 + 自动发布新版本；SSE 事件 agent.update 带 action=published|rolled_back）

## Automations（自动化规则，v1.1 新增）
### POST /automations
body `{"agent_id":1,"name":"晨会摘要","cron_expr":"30 9 * * 1-5","title":"生成今日晨会摘要","prompt":"…","priority":6}`
→ `{"id":1,"next_run":"2026-11-09T09:30:00+08:00"}`（服务端校验 cron 并计算 next_run，客户端不传调度状态）
### GET /automations?enabled=true → items:
`{"id","agent_id","agent_name","name","cron_expr","title","prompt","priority","enabled","last_run","next_run","created_at"}`
### GET /automations/{id} | PATCH /automations/{id}（部分更新；改 cron_expr 会重算 next_run）| DELETE /automations/{id}
### POST /automations/{id}/enable | /automations/{id}/disable → `{"id":1,"enabled":true}`
SSE 事件：`automation.fired`（触发建任务）/ `automation.update`（CRUD 变更）

### POST /agents/{id}/clone（v1.4 新增，I3）body 可选 `{"name":"自定义名"}`
→ `{"id","name","cloned_from"}`；配置全复制、名称自动去重、全新版本链（v1=源当前配置）、默认启用

### GET /models（v1.4 模型可选化）→ `{"models":[{"name","provider":"ollama|builtin","available":bool}]}`
动态发现：Ollama `/api/tags` 已装列表 + 内置 mock，不硬编码。Agent 表单据此渲染下拉框（后端不可达时前端降级为自由输入）。

### POST /chat/stream（v1.4 新增，F15 流式）body 同 /chat
响应为 chunked text 流：正文为增量文本片段；末行 `\n\n@@META@@{conversation_id, tokens, error}`。
会话落库与用量计量与同步版一致（purpose=chat）。前端用 fetch + ReadableStream 消费。

## 会话回放（v1.4 新增，F6）
### GET /conversations → `{"items":[{"id","agent_id","agent_name","title","message_count","run_count","created_at"}],"total":n}`（最新在前，limit 50）
### GET /conversations/{id} → `{"id","agent_id","agent_name","title","created_at","messages":[{id,role,content,created_at}],"runs":[…]}`

## 执行环境维度（v1.4 新增：对标 Multica worktree / Orca artifacts / compound skills）
### Workspaces：POST /workspaces `{"name","root_path","branch","note"}` → `{"id"}`；GET /workspaces?active=true；GET/PATCH/DELETE /workspaces/{id}
任务创建时 `workspace_id` 绑定；agent 在绑定环境中执行（执行副本注入 `[执行环境]` 段，不污染任务指令）。
### Artifacts：GET /artifacts?task_id= → `{"items":[{id,task_id,run_id,kind,name,mime_type,size,created_at}]}`；GET /artifacts/{id}（含 content）
产物 kind：report / patch / diff / data / log。runner 产出自动落库（周报类任务出 report、绑定环境任务出执行 log、LLM 长回复自动固化报告）；SSE 事件 `task.artifacts`。
### Skills：POST /skills `{"name","content","tags"}` → `{"id"}`；GET /skills?tag=（按引用次数排序）；GET/PATCH/DELETE /skills/{id}
任务创建时 `skill_ids` 引用；执行时注入 `[经验参考]` 段到**执行副本**（指令通道纯净）并累加引用计数。

## 前端页面映射
| 路由 | 页面 | 接口 |
|---|---|---|
| `/` | 舰队总览（agent 卡片墙 + 实时事件流） | /agents /events(SSE) /health |
| `/tasks` | 任务看板（kanban：queued/running/review/done/failed，拖拽改优先级可简化为按钮） | /tasks* |
| `/tasks/:id` | 任务详情 + run 列表 + Trace 瀑布图 | /tasks/{id} /runs/{id} |
| `/agents/:id` | Agent 详情（配置编辑 + 统计 + 近期 runs） | /agents/{id} |
| `/chat` | Agent 试聊台 | /chat |
| `/evals` | 评测中心（发起/报告/对比） | /evals* /eval-datasets |
| `/usage` | 用量看板 | /usage |
