-- 065_memory_record.up.sql
-- Canonical Memory record (per memory.Memory in Go). Mirrors the
-- unified record from the user-supplied reference doc §四.
--
-- Hard rule: raw_kind + raw_id is the pointer to the original row in
-- file_index / thread_context_item / message / artifact. We NEVER copy
-- the raw payload here — Body is the derived/summary form. Polymorphic
-- FK so no SQL constraint; service layer validates existence on Append.
--
-- Phase 3 will add memory_chunk (vector embeddings) referencing
-- memory_record.id.

CREATE TABLE IF NOT EXISTS memory_record (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id  UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    type          TEXT NOT NULL CHECK (type IN
                    ('fact','summary','transcript','task',
                     'decision','profile','context')),
    scope         TEXT NOT NULL CHECK (scope IN
                    ('private_local','shared_summary','team',
                     'agent_state','archive')),
    source        TEXT NOT NULL,                  -- meeting|chat|manual|file|agent
    raw_kind      TEXT NOT NULL CHECK (raw_kind IN
                    ('file_index','thread_context_item','message','artifact')),
    raw_id        UUID NOT NULL,
    summary       TEXT,
    body          TEXT,
    tags          TEXT[] NOT NULL DEFAULT '{}',
    entities      TEXT[] NOT NULL DEFAULT '{}',
    confidence    REAL  NOT NULL DEFAULT 0.5
                    CHECK (confidence >= 0 AND confidence <= 1),
    status        TEXT NOT NULL DEFAULT 'candidate' CHECK (status IN
                    ('candidate','confirmed','archived')),
    version       INT  NOT NULL DEFAULT 1,
    created_by    UUID,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_memory_workspace_type
    ON memory_record (workspace_id, type, status, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_memory_workspace_scope
    ON memory_record (workspace_id, scope, status);

CREATE INDEX IF NOT EXISTS idx_memory_raw
    ON memory_record (raw_kind, raw_id);

CREATE INDEX IF NOT EXISTS idx_memory_tags
    ON memory_record USING GIN (tags);

CREATE INDEX IF NOT EXISTS idx_memory_entities
    ON memory_record USING GIN (entities);
