-- No-op: the forward migration is a one-shot data backfill. We cannot
-- restore the pre-backfill NULL values without a snapshot, and setting
-- every newly-assigned task back to NULL would wipe manual edits made
-- after the migration ran. If a rollback is truly needed, identify the
-- affected rows by created_at < <migration_apply_time> and update them
-- manually with domain knowledge.
SELECT 1 WHERE false;
