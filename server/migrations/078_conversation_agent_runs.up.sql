CREATE TABLE IF NOT EXISTS conversation_agent_run (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
  trigger_message_id UUID NOT NULL REFERENCES message(id) ON DELETE CASCADE,
  response_message_id UUID NULL REFERENCES message(id) ON DELETE SET NULL,
  agent_id UUID NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
  runtime_id UUID NOT NULL REFERENCES agent_runtime(id) ON DELETE CASCADE,
  peer_user_id UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
  provider TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'queued',
  prompt TEXT NOT NULL,
  output TEXT NOT NULL DEFAULT '',
  session_id TEXT NULL,
  work_dir TEXT NULL,
  error_message TEXT NULL,
  metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
  claimed_at TIMESTAMPTZ NULL,
  started_at TIMESTAMPTZ NULL,
  completed_at TIMESTAMPTZ NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT conversation_agent_run_status_check CHECK (
    status IN ('queued', 'claimed', 'running', 'completed', 'failed', 'cancelled')
  )
);

CREATE INDEX IF NOT EXISTS idx_conversation_agent_run_runtime_queue
  ON conversation_agent_run(runtime_id, status, created_at);

CREATE INDEX IF NOT EXISTS idx_conversation_agent_run_trigger
  ON conversation_agent_run(trigger_message_id);

CREATE INDEX IF NOT EXISTS idx_conversation_agent_run_peer
  ON conversation_agent_run(workspace_id, agent_id, peer_user_id, created_at DESC);

CREATE TABLE IF NOT EXISTS conversation_agent_run_event (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  run_id UUID NOT NULL REFERENCES conversation_agent_run(id) ON DELETE CASCADE,
  seq BIGINT NOT NULL,
  type TEXT NOT NULL,
  content TEXT NULL,
  tool TEXT NULL,
  input JSONB NULL,
  output TEXT NULL,
  error TEXT NULL,
  metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT conversation_agent_run_event_unique_seq UNIQUE(run_id, seq)
);

CREATE INDEX IF NOT EXISTS idx_conversation_agent_run_event_run_seq
  ON conversation_agent_run_event(run_id, seq);
