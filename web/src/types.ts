// agentfleet API Contract v1 类型定义（字段名与 docs/api-contract.md 一字不差）

// ---------- Health ----------
export interface LlmInfo {
  mode: 'ollama' | 'mock';
  model: string;
}
export interface Health {
  status: string;
  db: boolean;
  scheduler: boolean;
  llm: LlmInfo;
  uptime_s: number;
}

// ---------- Agents ----------
export type Runner = 'llm' | 'mock';
export interface AgentSummary {
  id: number;
  name: string;
  role: string;
  model: string;
  runner: Runner;
  enabled: boolean;
  max_concurrency: number;
  running: number;
  queued: number;
  created_at: string;
}
export interface AgentStats {
  total_runs: number;
  success_rate: number;
  avg_latency_ms: number;
  tokens_30d: number;
}
export interface AgentDetail extends AgentSummary {
  system_prompt: string;
  tools: string[];
  stats: AgentStats;
  // I14 草稿/发布双态
  draft_config?: Record<string, unknown> | null;
  has_unpublished_changes?: boolean;
}
export interface AgentPatch {
  name?: string;
  role?: string;
  system_prompt?: string;
  model?: string;
  tools?: string[];
  max_concurrency?: number;
}

// ---------- Tasks ----------
export type TaskStatus = 'queued' | 'running' | 'review' | 'done' | 'failed' | 'canceled';
export interface TaskSummary {
  id: number;
  title: string;
  status: TaskStatus;
  priority: number;
  agent_id: number;
  agent_name: string;
  created_at: string;
  started_at: string | null;
  finished_at: string | null;
  blocked_by: number[];
}
export interface TaskDep {
  id: number;
  title: string;
  status: TaskStatus;
}
export interface TaskDagNode {
  id: number;
  title: string;
  status: TaskStatus;
}
export interface TaskDagEdge {
  from: number;
  to: number;
}
export interface TaskDag {
  nodes: TaskDagNode[];
  edges: TaskDagEdge[];
  cycle: boolean;
}
export interface TaskDetail extends TaskSummary {
  prompt: string;
  result: string | Record<string, unknown> | null;
  error: string | null;
  deps: TaskDep[];
}
export interface TaskCreate {
  title: string;
  prompt: string;
  agent_id: number;
  priority: number;
  depends_on: number[];
}
export interface TaskCreated {
  id: number;
  status: string;
}

// ---------- Runs ----------
export interface RunSummary {
  id: number;
  parent_run_id?: number; // I17：handoff 链父 run（0/缺省=链头）
  status: string;
  started_at: string | null;
  finished_at: string | null;
  total_tokens: number;
  cost_usd: number;
}
export type StepKind = 'llm' | 'tool' | 'handoff' | 'gate';
export interface RunStep {
  seq: number;
  parent_seq?: number; // I17：父步 seq（循环观察步挂 decide 下；0/缺省=顶层）
  kind: StepKind | string;
  name: string;
  input?: unknown;
  output?: unknown;
  note?: string;
  latency_ms: number;
  tokens?: number;
}
export interface RunTrace {
  id: number;
  task_id: number;
  agent_id: number;
  status: string;
  total_tokens: number;
  cost_usd: number;
  steps: RunStep[];
}

// ---------- Chat ----------
export interface ChatResponse {
  conversation_id: number;
  reply: string;
  run_id: number;
}

// ---------- Usage ----------
export interface UsageByModel {
  model: string;
  requests: number;
  tokens: number;
}
export interface UsageByAgent {
  agent_id: number;
  tokens: number;
}
export interface Usage {
  window: string;
  requests: number;
  prompt_tokens: number;
  completion_tokens: number;
  cost_usd?: number;
  cost_by_model?: Record<string, number>;
  cost_by_agent?: Record<string, number>;
  by_model: UsageByModel[];
  by_agent: UsageByAgent[];
  rate_limit?: RateLimitStats;
}

// 网关限流状态（v1.4，F8-b）
export interface RateLimitStats {
  enabled: boolean;
  per_minute: number;
  rejected_total: number;
}

// ---------- Eval ----------
export interface EvalSummary {
  id: number;
  dataset_id: number;
  agent_id: number;
  status: string;
  score_avg: number | null;
  created_at: string;
}
export interface EvalCase {
  case_id: string;
  score: number;
  pass: boolean;
  judge_comment: string;
}
export interface EvalReport {
  cases: EvalCase[];
  score_avg: number;
  pass_rate: number;
}
export interface EvalDetail {
  id: number;
  status: string;
  report: EvalReport | null;
}
export interface EvalDataset {
  id: number;
  name: string;
  description: string;
  cases: number;
}

// ---------- 分页 ----------
export interface Page<T> {
  items: T[];
  total: number;
  page: number;
  page_size: number;
}

// ---------- SSE 事件 ----------
export interface FleetEvent {
  seq?: number;
  kind: string;
  data?: Record<string, unknown>;
}

// ---------- Automations（自动化规则，v1.1） ----------
export interface Automation {
  id: number;
  agent_id: number;
  agent_name?: string;
  name: string;
  cron_expr: string;
  title: string;
  prompt: string;
  priority: number;
  enabled: boolean;
  last_run: string | null;
  next_run: string | null;
  created_at: string;
}
export interface AutomationCreate {
  agent_id: number;
  name: string;
  cron_expr: string;
  title: string;
  prompt: string;
  priority: number;
}
export interface AutomationCreated {
  id: number;
  next_run: string;
}

// ---------- 模型可选化（v1.4） ----------
export interface ModelInfo {
  name: string;
  provider: string; // ollama | builtin
  available: boolean;
}

// ---------- Multica 对标（v1.5） ----------
export interface Project {
  id: number;
  name: string;
  description: string;
  repos: string[];
  created_at: string;
}
export interface TaskComment {
  id: number;
  task_id: number;
  author_type: string;
  author_name: string;
  content: string;
  created_at: string;
}

// ---------- Agent 版本管理（v1.2，F2） ----------
export interface AgentVersion {
  id: number;
  agent_id: number;
  version: number;
  config: Record<string, unknown>;
  note: string;
  created_at: string;
}

// ---------- 会话回放（v1.4，F6） ----------
export interface ConversationSummary {
  id: number;
  agent_id: number;
  agent_name?: string;
  title: string;
  message_count: number;
  run_count: number;
  created_at: string;
}
export interface ReplayMessage {
  id: number;
  role: string;
  content: string;
  created_at: string;
}
export interface ReplayRun {
  run_id: number;
  task_id: number;
  task_title?: string;
  status: string;
  total_tokens: number;
  started_at: string;
}
export interface ConversationDetail {
  id: number;
  agent_id: number;
  agent_name?: string;
  title: string;
  created_at: string;
  messages: ReplayMessage[];
  runs: ReplayRun[];
}

// ---------- 执行环境维度（v1.4）：artifacts / workspaces / skills ----------
export interface Artifact {
  id: number;
  task_id: number;
  run_id: number;
  kind: string;
  name: string;
  mime_type: string;
  size: number;
  content?: string;
  created_at: string;
}
export interface Workspace {
  id: number;
  name: string;
  root_path: string;
  branch: string;
  note: string;
  status: string;
  created_at: string;
}
export interface Skill {
  id: number;
  name: string;
  content: string;
  tags: string[];
  usage_count: number;
  source_task?: number;
  created_at: string;
  updated_at: string;
}
