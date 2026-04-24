DROP INDEX IF EXISTS idx_runs_finished_at;

ALTER TABLE runs
    DROP COLUMN IF EXISTS comparisons_count,
    DROP COLUMN IF EXISTS sources_used,
    DROP COLUMN IF EXISTS subjects,
    DROP COLUMN IF EXISTS anchor_block;
