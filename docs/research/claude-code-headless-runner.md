# 调研：Claude Code headless 驱动机制（外部 agent 进程协议）

> 2026-11-11 · 深度组件冲刺 B 切片 · 对标对象：Claude Code `-p` headless / Agent SDK。
> 动机：「真 coding-agent runner」（接外部 agent 进程）与 I16 Local Runtime 的执行体选型——
> 本篇回答「外部 agent 进程如何被平台驱动」：协议、续跑、权限、治理。

## 一、三种输出协议

| 格式 | 形态 | 用途 |
|------|------|------|
| text | 纯文本 | 人读 |
| json | 单对象：`result` + `session_id` + `total_cost_usd`（按模型分账） | 一次性任务捕获会话与成本 |
| stream-json | 换行分隔事件流，末行 `result` | 实时 trace / 管道编排 |

- `--json-schema`：结构化输出在 `structured_output` 字段（模型拒绝时缺失——脚本先查存在性）。
- 对 agentfleet：**stream-json 行解析与 F15 StreamChat NDJSON 经验同构**，runner 侧复用成熟路径。

## 二、会话续跑（与 I11 互补的第二层 resume）

- `--continue`：续最近会话；`--resume <session_id>`（可跨目录按 ID 查）。
- SIGTERM 中断后恢复默认保留被中断的 turn（`CLAUDE_CODE_RESUME_INTERRUPTED_TURN=1` 继续）。
- **分层结论：session 级续跑（会话状态在 agent 进程内）与 I11 步级结果重放（观察在平台侧）互补**
  ——平台重放确定性工具观察，外部 agent 续跑自己的对话记忆，两层各自成立。

## 三、权限档位（映射审批门）

- `--permission-mode`：**auto**（分类器自动审）/ **dontAsk**（需提示的一律拒绝，锁死 CI）/
  **acceptEdits**（自动批准文件写入，其余仍需规则）。
- `--allowedTools "Bash,Read,Edit"` 预批准 + 规则语法 `Bash(git commit *)`（`*` 前空格敏感）。
- 无人值守：`--permission-prompts none` 直接拒绝并告知不要重试。
- **对 agentfleet：I29/I33 风险分级可直接映射**——low=dontAsk+白名单、medium=acceptEdits、
  high=任务级审批门（已有）+ 工具级闸（已有）。审批语义跨 runner 通用。

## 四、stream-json 事件类型（trace 对接面）

- `system/init`：首事件，含模型/工具/MCP 服务器/插件/**capabilities 数组**（特性探测）。
- `system/api_retry`：attempt/max_retries/retry_delay_ms/error_status（限流可见）。
- 子代理消息靠 `parent_tool_use_id` 区分归属（主对话 null）——**与 I17 parent_run_id 同构确认**。
- 末行 `result`：最终文本 + 成本 + 会话元数据。

## 五、治理细节

- 退出码：成功 0；**SIGTERM=143 且 turn 不完成**——优雅中断用 SIGINT/SDK interrupt()。
- stdin 管道上限 10MB；后台任务空闲等待 10 分钟（ceiling 可调）；`--max-budget-usd` 预算熔断
  （与 F5 token 预算语义同族，金额维度）。
- **`--bare` 模式**：跳过 hooks/skills/插件/MCP 自动发现——启动快、结果可复现，
  官方定位「脚本/SDK 调用推荐模式」。平台驱动外部 agent 应默认 bare + 显式注入。

## 六、对 agentfleet 的借鉴 → I34 入池

1. **I34 ClaudeCodeRunner**（I16 Local Runtime 的执行体）：exec `claude --bare -p` +
   stream-json 行解析（复用 F15 经验）+ `session_id` 落库（session 续跑）+
   `total_cost_usd` 直连 F10 成本核算 + permission-mode 映射 I29 风险分级。
2. **parent_tool_use_id ↔ parent_run_id 同构**：I17 层级数据模型是通用形态，外部 runner
   的子代理层级可直接映射。
3. **`--bare` 可复现原则**：平台驱动的 runner 执行应显式声明全部依赖（与指令通道纯净性同原则）。
4. **SIGTERM/SIGINT 语义**：优雅停机 drain（已有）对外部子进程要选对信号——杀错信号会丢 turn。

## 七、明确不抄

- 权限分类器（auto 的自动审查）：安全敏感且重模型，我们用规则 fast-path + 任务级预审（F16）。
- 插件/技能自动发现：平台侧已有 Skills 体系，外部进程用 bare 显式注入。
- OAuth/keychain 凭证链：本地单用户不需要。

## 八、来源

- [Run Claude Code programmatically（官方）](https://code.claude.com/docs/en/headless)
- [Issue #53584：stream-json 挂起已知 bug](https://github.com/anthropics/claude-code/issues/53584)
- [CLI Control Surface：print mode/JSON/sessions](https://claude-world.com/tutorials/s13-control-protocol)
- [Wrapping Claude CLI for Agentic Applications](https://avasdream.com/blog/claude-cli-agentic-wrapper)
