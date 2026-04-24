-- Run execution summary — captures "what did this run actually look at"
-- so operators can answer "which blocks/addresses got verified?" without
-- persisting per-comparison rows. Discrepancies still live in their own
-- table for failure detail; this is the equivalent lens for *successful*
-- comparisons, summarised at the Run level.
--
-- anchor_block       block-anchored metrics resolve against this tip
--                    (NULL for Snapshot-only runs that have no anchor)
-- subjects           JSONB array of compared entities. Each element:
--                      {"k":"block","b":<u64>}
--                      {"k":"address_latest","a":"0x..."}
--                      {"k":"address_at_block","a":"0x...","b":<u64>}
--                      {"k":"erc20_balance","a":"0x...","t":"0x..."}
--                      {"k":"erc20_holdings","a":"0x..."}
--                      {"k":"snapshot","n":"<metric-key>"}
--                    Short keys intentionally — rows accumulate.
-- sources_used       IDs of sources that participated. Snapshotted at
--                    run time so a later sources-table edit doesn't
--                    mutate historical rows.
-- comparisons_count  Total # of (subject × metric × source-pair)
--                    comparisons attempted. Rough fidelity indicator.

ALTER TABLE runs
    ADD COLUMN anchor_block      BIGINT,
    ADD COLUMN subjects          JSONB  NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN sources_used      TEXT[] NOT NULL DEFAULT '{}',
    ADD COLUMN comparisons_count INTEGER NOT NULL DEFAULT 0;

-- Retention sweep (`maintenance:prune_old_runs`) filters by
-- finished_at — a partial index keeps it cheap as the table grows.
CREATE INDEX IF NOT EXISTS idx_runs_finished_at
    ON runs (finished_at)
    WHERE finished_at IS NOT NULL;
