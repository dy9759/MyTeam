ALTER TABLE channel ADD COLUMN IF NOT EXISTS founder_id UUID REFERENCES "user"(id);
UPDATE channel SET founder_id = created_by WHERE founder_id IS NULL;
