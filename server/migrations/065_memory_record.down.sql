-- Reverse 065_memory_record.up.sql.

DROP INDEX IF EXISTS idx_memory_entities;
DROP INDEX IF EXISTS idx_memory_tags;
DROP INDEX IF EXISTS idx_memory_raw;
DROP INDEX IF EXISTS idx_memory_workspace_scope;
DROP INDEX IF EXISTS idx_memory_workspace_type;
DROP TABLE IF EXISTS memory_record;
