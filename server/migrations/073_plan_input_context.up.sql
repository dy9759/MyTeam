-- Plan Phase 3: first-class plan context fields.
--
-- Until now the generator only carried chat references on
-- project.source_conversations; user-uploaded files and free-form
-- form fields had nowhere to live. The 计划 tab already renders
-- placeholder panels for these, so wire them through the schema.
--
-- input_files   — JSONB array of {id, name, mime?} entries. IDs
--                 reference file_index rows; we keep them as payload
--                 rather than a join table because the list is
--                 plan-local and never queried across plans.
-- user_inputs   — JSONB object of arbitrary string-keyed values the
--                 user supplied when spinning up the project (brand
--                 name, target metric, etc). Kept as an object so the
--                 generator prompt can read it as "key: value" pairs.

ALTER TABLE plan
    ADD COLUMN IF NOT EXISTS input_files JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS user_inputs JSONB NOT NULL DEFAULT '{}'::jsonb;
