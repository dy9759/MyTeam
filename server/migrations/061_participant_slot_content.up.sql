ALTER TABLE participant_slot
  ADD COLUMN IF NOT EXISTS content JSONB;
