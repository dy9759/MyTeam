ALTER TABLE plan
    DROP COLUMN IF EXISTS user_inputs,
    DROP COLUMN IF EXISTS input_files;
