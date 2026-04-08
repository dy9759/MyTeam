CREATE TABLE IF NOT EXISTS file_index (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    uploader_identity_id UUID NOT NULL,
    uploader_identity_type TEXT NOT NULL,
    owner_id UUID NOT NULL,
    source_type TEXT NOT NULL,
    source_id UUID NOT NULL,
    file_name TEXT NOT NULL,
    file_size BIGINT,
    content_type TEXT,
    storage_path TEXT,
    access_scope JSONB DEFAULT '{}',
    channel_id UUID,
    project_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_file_index_workspace ON file_index(workspace_id);
CREATE INDEX idx_file_index_owner ON file_index(owner_id);
CREATE INDEX idx_file_index_source ON file_index(source_type, source_id);
CREATE INDEX idx_file_index_project ON file_index(project_id);
CREATE INDEX idx_file_index_channel ON file_index(channel_id);

CREATE TABLE IF NOT EXISTS file_snapshot (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    file_id UUID NOT NULL,
    snapshot_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    storage_path TEXT NOT NULL,
    referenced_by JSONB DEFAULT '[]',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_file_snapshot_file ON file_snapshot(file_id);
