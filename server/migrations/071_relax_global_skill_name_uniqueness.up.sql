-- Relax skill name uniqueness for globals.
--
-- Migration 069 created uq_skill_global_name as a partial unique index
-- over skill.name WHERE is_global = true. That assumed a single global
-- namespace, which breaks as soon as two bundle sources ship a skill
-- with the same slug (e.g. addyosmani/.../test-driven-development and
-- superpowers/.../test-driven-development — the loader hits SQLSTATE
-- 23505 on the second insert and startup fails).
--
-- Bundle entries are already uniquely addressed by source_ref (see
-- uq_skill_bundle_ref), so we scope the cross-source name collision
-- to manual/upload globals and let bundle rows coexist freely.

DROP INDEX IF EXISTS uq_skill_global_name;

CREATE UNIQUE INDEX uq_skill_global_name
    ON skill(name)
    WHERE is_global = true AND source <> 'bundle';
