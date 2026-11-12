-- agentfleet 初始 schema（幂等：CREATE IF NOT EXISTS）
CREATE TABLE IF NOT EXISTS agents (
  id              BIGSERIAL PRIMARY KEY,
  name            TEXT NOT NULL,
  role            TEXT NOT NULL DEFAULT '',
  system_prompt   TEXT NOT NULL DEFAULT '',
  model           TEXT NOT NULL DEFAULT 'qwen3.5',
  tools           JSONB NOT NULL DEFAULT '[]',
  runner          TEXT NOT NULL DEFAULT 'llm',
  max_concurrency INT NOT NULL DEFAULT 1,
  token_budget_24h INT NOT NULL DEFAULT 0,
  enabled         BOOLEAN NOT NULL DEFAULT TRUE,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS tasks (
  id            BIGSERIAL PRIMARY KEY,
  title         TEXT NOT NULL,
  prompt        TEXT NOT NULL DEFAULT '',
  agent_id      BIGINT NOT NULL REFERENCES agents(id),
  priority      INT NOT NULL DEFAULT 5,
  status        TEXT NOT NULL DEFAULT 'queued',
  depends_on    JSONB NOT NULL DEFAULT '[]',
  handoff_count INT NOT NULL DEFAULT 0,
  max_handoffs  INT NOT NULL DEFAULT 3,
  retry_count   INT NOT NULL DEFAULT 0,
  max_retries   INT NOT NULL DEFAULT 2,
  visible_after TIMESTAMPTZ NOT NULL DEFAULT now(),
  result        TEXT NOT NULL DEFAULT '',
  error         TEXT NOT NULL DEFAULT '',
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  started_at    TIMESTAMPTZ,
  finished_at   TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS webhooks (
  id         BIGSERIAL PRIMARY KEY,
  name       TEXT NOT NULL,
  url        TEXT NOT NULL,
  secret     TEXT NOT NULL DEFAULT '',
  events     JSONB NOT NULL DEFAULT '["*"]',
  active     BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status, priority DESC, id);

CREATE TABLE IF NOT EXISTS runs (
  id              BIGSERIAL PRIMARY KEY,
  task_id         BIGINT NOT NULL,
  agent_id        BIGINT NOT NULL,
  conversation_id BIGINT,
  status          TEXT NOT NULL DEFAULT 'running',
  total_tokens    INT NOT NULL DEFAULT 0,
  cost_usd        DOUBLE PRECISION NOT NULL DEFAULT 0,
  error           TEXT NOT NULL DEFAULT '',
  started_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  finished_at     TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_runs_task ON runs(task_id);

CREATE TABLE IF NOT EXISTS run_steps (
  run_id     BIGINT NOT NULL REFERENCES runs(id),
  seq        INT NOT NULL,
  kind       TEXT NOT NULL,
  name       TEXT NOT NULL DEFAULT '',
  input      JSONB,
  output     JSONB,
  note       TEXT NOT NULL DEFAULT '',
  latency_ms BIGINT NOT NULL DEFAULT 0,
  tokens     INT NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (run_id, seq)
);

CREATE TABLE IF NOT EXISTS conversations (
  id         BIGSERIAL PRIMARY KEY,
  agent_id   BIGINT NOT NULL,
  title      TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS messages (
  id              BIGSERIAL PRIMARY KEY,
  conversation_id BIGINT NOT NULL REFERENCES conversations(id),
  role            TEXT NOT NULL,
  content         TEXT NOT NULL,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- I 系列 additive 列（目标表已在上方创建；曾因 ALTER 前置于 CREATE 导致全新部署 migrate 失败）

CREATE TABLE IF NOT EXISTS usage_events (
  id                BIGSERIAL PRIMARY KEY,
  agent_id          BIGINT NOT NULL DEFAULT 0,
  agent_name        TEXT NOT NULL DEFAULT '',
  model             TEXT NOT NULL DEFAULT '',
  prompt_tokens     INT NOT NULL DEFAULT 0,
  completion_tokens INT NOT NULL DEFAULT 0,
  requests          INT NOT NULL DEFAULT 1,
  created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_usage_created ON usage_events(created_at);

CREATE TABLE IF NOT EXISTS eval_datasets (
  id          BIGSERIAL PRIMARY KEY,
  name        TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS eval_cases (
  id         BIGSERIAL PRIMARY KEY,
  dataset_id BIGINT NOT NULL REFERENCES eval_datasets(id),
  input      TEXT NOT NULL,
  expected   JSONB NOT NULL DEFAULT '{}'
);

CREATE TABLE IF NOT EXISTS eval_runs (
  id         BIGSERIAL PRIMARY KEY,
  dataset_id BIGINT NOT NULL,
  agent_id   BIGINT NOT NULL,
  status     TEXT NOT NULL DEFAULT 'running',
  score_avg  DOUBLE PRECISION NOT NULL DEFAULT 0,
  pass_rate  DOUBLE PRECISION NOT NULL DEFAULT 0,
  report     JSONB,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS automations (
  id         BIGSERIAL PRIMARY KEY,
  agent_id   BIGINT NOT NULL REFERENCES agents(id),
  name       TEXT NOT NULL,
  cron_expr  TEXT NOT NULL,
  title      TEXT NOT NULL,
  prompt     TEXT NOT NULL DEFAULT '',
  priority   INT NOT NULL DEFAULT 5,
  enabled    BOOLEAN NOT NULL DEFAULT TRUE,
  last_run   TIMESTAMPTZ,
  next_run   TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS agent_versions (
  id         BIGSERIAL PRIMARY KEY,
  agent_id   BIGINT NOT NULL REFERENCES agents(id),
  version    INT NOT NULL,
  config     JSONB NOT NULL DEFAULT '{}',
  note       TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (agent_id, version)
);

CREATE TABLE IF NOT EXISTS workspaces (
  id         BIGSERIAL PRIMARY KEY,
  name       TEXT NOT NULL,
  root_path  TEXT NOT NULL DEFAULT '',
  branch     TEXT NOT NULL DEFAULT '',
  note       TEXT NOT NULL DEFAULT '',
  status     TEXT NOT NULL DEFAULT 'active',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS artifacts (
  id         BIGSERIAL PRIMARY KEY,
  task_id    BIGINT NOT NULL,
  run_id     BIGINT NOT NULL DEFAULT 0,
  kind       TEXT NOT NULL DEFAULT 'report',
  name       TEXT NOT NULL,
  mime_type  TEXT NOT NULL DEFAULT 'text/plain',
  size       INT NOT NULL DEFAULT 0,
  content    TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_artifacts_task ON artifacts(task_id);

CREATE TABLE IF NOT EXISTS skills (
  id           BIGSERIAL PRIMARY KEY,
  name         TEXT NOT NULL,
  content      TEXT NOT NULL,
  tags         JSONB NOT NULL DEFAULT '[]',
  usage_count  INT NOT NULL DEFAULT 0,
  source_task  BIGINT NOT NULL DEFAULT 0,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS api_keys (
  id            BIGSERIAL PRIMARY KEY,
  name          TEXT NOT NULL,
  key_hash      TEXT NOT NULL UNIQUE,
  key_prefix    TEXT NOT NULL DEFAULT '',
  scopes        TEXT NOT NULL DEFAULT 'all',
  last_used_at  TIMESTAMPTZ,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS squads (
  id           BIGSERIAL PRIMARY KEY,
  name         TEXT NOT NULL,
  description  TEXT NOT NULL DEFAULT '',
  leader_id    BIGINT NOT NULL REFERENCES agents(id),
  instructions TEXT NOT NULL DEFAULT '',
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS squad_members (
  id          BIGSERIAL PRIMARY KEY,
  squad_id    BIGINT NOT NULL REFERENCES squads(id),
  member_type TEXT NOT NULL DEFAULT 'agent',
  member_id   BIGINT NOT NULL,
  role        TEXT NOT NULL DEFAULT '',
  UNIQUE (squad_id, member_type, member_id)
);

CREATE TABLE IF NOT EXISTS projects (
  id          BIGSERIAL PRIMARY KEY,
  name        TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  repos       JSONB NOT NULL DEFAULT '[]',
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS task_comments (
  id          BIGSERIAL PRIMARY KEY,
  task_id     BIGINT NOT NULL,
  author_type TEXT NOT NULL DEFAULT 'member',
  author_name TEXT NOT NULL DEFAULT '',
  content     TEXT NOT NULL,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_task_comments ON task_comments(task_id);

CREATE TABLE IF NOT EXISTS events (
  seq        BIGSERIAL PRIMARY KEY,
  kind       TEXT NOT NULL,
  data       JSONB NOT NULL DEFAULT '{}',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ---- additive 列统一追加（必须在全部 CREATE TABLE 之后——曾因前置导致全新部署 migrate 失败）----
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS workspace_id BIGINT NOT NULL DEFAULT 0;
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS skill_ids JSONB NOT NULL DEFAULT '[]';
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS risk_level TEXT NOT NULL DEFAULT '';
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS project_id BIGINT NOT NULL DEFAULT 0;
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS labels JSONB NOT NULL DEFAULT '[]';
ALTER TABLE agents ADD COLUMN IF NOT EXISTS draft_config JSONB;
ALTER TABLE agents ADD COLUMN IF NOT EXISTS ramping_version INT NOT NULL DEFAULT 0;
ALTER TABLE agents ADD COLUMN IF NOT EXISTS ramp_pct INT NOT NULL DEFAULT 0;
ALTER TABLE run_steps ADD COLUMN IF NOT EXISTS parent_seq BIGINT NOT NULL DEFAULT 0;
ALTER TABLE runs ADD COLUMN IF NOT EXISTS parent_run_id BIGINT NOT NULL DEFAULT 0;
ALTER TABLE runs ADD COLUMN IF NOT EXISTS agent_version INT NOT NULL DEFAULT 0;
ALTER TABLE runs ADD COLUMN IF NOT EXISTS session_id TEXT NOT NULL DEFAULT '';
ALTER TABLE run_steps ADD COLUMN IF NOT EXISTS deterministic BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE workspaces ADD COLUMN IF NOT EXISTS runtime_status TEXT NOT NULL DEFAULT '';
ALTER TABLE workspaces ADD COLUMN IF NOT EXISTS runtime_note TEXT NOT NULL DEFAULT '';
ALTER TABLE webhooks ADD COLUMN IF NOT EXISTS last_status TEXT NOT NULL DEFAULT '';
ALTER TABLE webhooks ADD COLUMN IF NOT EXISTS last_delivery_at TIMESTAMPTZ;
ALTER TABLE usage_events ADD COLUMN IF NOT EXISTS purpose TEXT NOT NULL DEFAULT 'execute';
ALTER TABLE workspaces ADD COLUMN IF NOT EXISTS runtime_note TEXT NOT NULL DEFAULT '';
ALTER TABLE webhooks ADD COLUMN IF NOT EXISTS last_status TEXT NOT NULL DEFAULT '';
ALTER TABLE webhooks ADD COLUMN IF NOT EXISTS last_delivery_at TIMESTAMPTZ;
ALTER TABLE usage_events ADD COLUMN IF NOT EXISTS purpose TEXT NOT NULL DEFAULT 'execute';
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS steer_note TEXT NOT NULL DEFAULT '';
ALTER TABLE conversations ADD COLUMN IF NOT EXISTS mode TEXT NOT NULL DEFAULT 'single';
ALTER TABLE messages ADD COLUMN IF NOT EXISTS agent_name TEXT NOT NULL DEFAULT '';
