import type {
  AgentDetail,
  AgentPatch,
  AgentSummary,
  AgentVersion,
  Artifact,
  Automation,
  AutomationCreate,
  AutomationCreated,
  ChatResponse,
  ConversationDetail,
  ConversationSummary,
  EvalDataset,
  EvalDetail,
  EvalSummary,
  Health,
  ModelInfo,
  Page,
  RunSummary,
  RunTrace,
  Skill,
  TaskCreate,
  TaskCreated,
  TaskDetail,
  TaskStatus,
  TaskSummary,
  Usage,
  Workspace,
} from './types';

const BASE = '/api/v1';

export class ApiError extends Error {
  status: number;
  code: string;
  constructor(status: number, code: string, message: string) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.code = code;
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  let res: Response;
  try {
    res = await fetch(`${BASE}${path}`, {
      ...init,
      headers: init?.body ? { 'Content-Type': 'application/json' } : undefined,
    });
  } catch {
    // fetch 网络层失败（后端不可达 / proxy 拒绝）
    throw new ApiError(0, 'network_error', '无法连接后端服务（localhost:8090）');
  }
  if (!res.ok) {
    let code = 'http_error';
    let message = `HTTP ${res.status}`;
    try {
      const body = (await res.json()) as { error?: { code?: string; message?: string } };
      if (body?.error) {
        code = body.error.code ?? code;
        message = body.error.message ?? message;
      }
    } catch {
      // 非 JSON 错误体，保留默认 message
    }
    throw new ApiError(res.status, code, message);
  }
  return (await res.json()) as T;
}

function post<T>(path: string, body?: unknown): Promise<T> {
  return request<T>(path, { method: 'POST', body: body === undefined ? undefined : JSON.stringify(body) });
}

function qs(params: Record<string, string | number | undefined>): string {
  const pairs = Object.entries(params)
    .filter(([, v]) => v !== undefined && v !== '')
    .map(([k, v]) => `${k}=${encodeURIComponent(String(v))}`);
  return pairs.length ? `?${pairs.join('&')}` : '';
}

export const api = {
  // Health
  health: () => request<Health>('/health'),
  models: () => request<{ models: ModelInfo[] }>('/models'),

  // Agents
  listAgents: () => request<Page<AgentSummary>>('/agents?page_size=100'),
  getAgent: (id: number) => request<AgentDetail>(`/agents/${id}`),
  patchAgent: (id: number, body: AgentPatch) =>
    request<{ id: number }>(`/agents/${id}`, { method: 'PATCH', body: JSON.stringify(body) }),
  setAgentEnabled: (id: number, enabled: boolean) =>
    post<{ id: number; enabled: boolean }>(`/agents/${id}/${enabled ? 'enable' : 'disable'}`),

  // Tasks
  listTasks: (opts?: { status?: TaskStatus; agent_id?: number; page?: number; page_size?: number }) =>
    request<Page<TaskSummary>>(`/tasks${qs({ ...opts })}`),
  getTask: (id: number) => request<TaskDetail>(`/tasks/${id}`),
  createTask: (body: TaskCreate) => post<TaskCreated>('/tasks', body),
  taskAction: (id: number, action: 'retry' | 'cancel' | 'approve') =>
    post<Record<string, unknown>>(`/tasks/${id}/${action}`),

  // Runs
  listTaskRuns: (taskId: number) => request<RunSummary[]>(`/tasks/${taskId}/runs`),
  getRun: (id: number) => request<RunTrace>(`/runs/${id}`),

  // Chat
  chat: (body: { agent_id: number; message: string; conversation_id: number | null }) =>
    post<ChatResponse>('/chat', body),

  // Usage
  usage: (window: '24h' | '7d') => request<Usage>(`/usage${qs({ window })}`),

  // Eval
  listEvals: () => request<EvalSummary[]>('/evals'),
  getEval: (id: number) => request<EvalDetail>(`/evals/${id}`),
  createEval: (body: { agent_id: number; dataset_id: number }) =>
    post<{ id: number; status: string }>('/evals', body),
  listDatasets: () => request<EvalDataset[]>('/eval-datasets'),

  // Automations（v1.1）
  listAutomations: () => request<Page<Automation>>('/automations?page_size=100'),
  createAutomation: (body: AutomationCreate) => post<AutomationCreated>('/automations', body),
  patchAutomation: (id: number, body: Partial<AutomationCreate> & { enabled?: boolean }) =>
    request<{ id: number; next_run?: string }>(`/automations/${id}`, { method: 'PATCH', body: JSON.stringify(body) }),
  deleteAutomation: (id: number) =>
    request<{ id: number; deleted: boolean }>(`/automations/${id}`, { method: 'DELETE' }),
  setAutomationEnabled: (id: number, enabled: boolean) =>
    post<{ id: number; enabled: boolean }>(`/automations/${id}/${enabled ? 'enable' : 'disable'}`),

  // Agent 版本管理（v1.2，F2）
  publishAgent: (id: number, note: string) =>
    post<{ agent_id: number; version: number }>(`/agents/${id}/publish`, { note }),
  listAgentVersions: (id: number) =>
    request<Page<AgentVersion>>(`/agents/${id}/versions?page_size=50`),
  rollbackAgent: (id: number, version: number) =>
    post<{ agent_id: number; rolled_back_from: number; new_version: number }>(
      `/agents/${id}/versions/${version}/rollback`),

  // 会话回放（v1.4，F6）
  listConversations: () => request<Page<ConversationSummary>>('/conversations'),
  getConversation: (id: number) => request<ConversationDetail>(`/conversations/${id}`),

  // 执行环境维度（v1.4）
  listArtifacts: (taskId?: number) =>
    request<Page<Artifact>>(`/artifacts${taskId ? `?task_id=${taskId}` : '?page_size=100'}`),
  getArtifact: (id: number) => request<Artifact>(`/artifacts/${id}`),
  listWorkspaces: () => request<Page<Workspace>>('/workspaces?page_size=100'),
  createWorkspace: (body: { name: string; root_path?: string; branch?: string; note?: string }) =>
    post<{ id: number }>('/workspaces', body),
  deleteWorkspace: (id: number) =>
    request<{ id: number }>(`/workspaces/${id}`, { method: 'DELETE' }),
  listSkills: () => request<Page<Skill>>('/skills?page_size=100'),
  createSkill: (body: { name: string; content: string; tags?: string[] }) =>
    post<{ id: number }>('/skills', body),
  deleteSkill: (id: number) =>
    request<{ id: number }>(`/skills/${id}`, { method: 'DELETE' }),
};
