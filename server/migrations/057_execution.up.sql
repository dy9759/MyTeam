-- Plan 5: execution — per-attempt record for Project task execution.
-- Independent of agent_task_queue (Issue link); shares Daemon infra.

CREATE TABLE IF NOT EXISTS execution (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  task_id UUID NOT NULL REFERENCES task(id) ON DELETE CASCADE,
  run_id UUID NOT NULL REFERENCES project_run(id) ON DELETE CASCADE,
  slot_id UUID REFERENCES participant_slot(id) ON DELETE SET NULL,
  agent_id UUID NOT NULL REFERENCES agent(id) ON DELETE RESTRICT,
  runtime_id UUID NOT NULL REFERENCES agent_runtime(id) ON DELETE RESTRICT,
  attempt INTEGER NOT NULL DEFAULT 1,

  status TEXT NOT NULL DEFAULT 'queued'
    CHECK (status IN ('queued','claimed','running','completed','failed','cancelled','timed_out')),
  priority INTEGER NOT NULL DEFAULT 0,
  payload JSONB NOT NULL DEFAULT '{}',
  result JSONB,
  error TEXT,

  context_ref JSONB NOT NULL DEFAULT '{}',
  log_retention_policy TEXT NOT NULL DEFAULT '90d'
    CHECK (log_retention_policy IN ('7d','30d','90d','permanent')),
  logs_expires_at TIMESTAMPTZ,

  -- Cost tracking (mirrors agent_task_queue.cost_* added in 054)
  cost_input_tokens INTEGER NOT NULL DEFAULT 0,
  cost_output_tokens INTEGER NOT NULL DEFAULT 0,
  cost_usd NUMERIC(10,4) NOT NULL DEFAULT 0,
  cost_provider TEXT,

  claimed_at TIMESTAMPTZ,
  started_at TIMESTAMPTZ,
  completed_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Hot-path claim index per PRD §10.2 (FOR UPDATE SKIP LOCKED)
CREATE INDEX IF NOT EXISTS idx_execution_claim
  ON execution(runtime_id, priority DESC, created_at ASC)
  WHERE status = 'queued';

CREATE INDEX IF NOT EXISTS idx_execution_task ON execution(task_id, attempt DESC);
CREATE INDEX IF NOT EXISTS idx_execution_run ON execution(run_id, status);
