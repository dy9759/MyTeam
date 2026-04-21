-- Remove only the rows this migration inserted (tagged via
-- identity_card.seeded_from_subagent_id). Agents the user renamed or
-- otherwise mutated post-seed will still roll back cleanly because
-- the JSON tag stays on the row regardless of later column edits.
DELETE FROM agent
WHERE kind   = 'agent'
  AND source = 'manual'
  AND identity_card ? 'seeded_from_subagent_id';
