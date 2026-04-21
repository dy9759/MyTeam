-- Restore the stricter global-name uniqueness that covered bundle rows.
-- Rolling this back may fail if two bundle sources share a skill name,
-- which is the exact condition the forward migration was written for;
-- resolve collisions by renaming one source's skill in the bundle tree
-- before rolling back.
DROP INDEX IF EXISTS uq_skill_global_name;

CREATE UNIQUE INDEX uq_skill_global_name
    ON skill(name)
    WHERE is_global = true;
