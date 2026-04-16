ALTER TABLE plan ALTER COLUMN task_brief TYPE TEXT USING task_brief::text;
ALTER TABLE plan ALTER COLUMN task_brief SET DEFAULT NULL;
DROP TABLE IF EXISTS project_context;
