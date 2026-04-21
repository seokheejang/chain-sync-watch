-- Phase 6 initial schema.
-- Single source of truth — gorm AutoMigrate is NOT used in production;
-- all schema changes land here as new numbered migrations.

CREATE TABLE runs (
    id            TEXT PRIMARY KEY,
    chain_id      BIGINT NOT NULL,
    status        TEXT   NOT NULL,
    trigger_type  TEXT   NOT NULL,
    trigger_data  JSONB  NOT NULL,
    strategy_kind TEXT   NOT NULL,
    strategy_data JSONB  NOT NULL,
    metrics       TEXT[] NOT NULL,
    error_msg     TEXT   NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL,
    started_at    TIMESTAMPTZ,
    finished_at   TIMESTAMPTZ
);

CREATE INDEX idx_runs_chain_status ON runs(chain_id, status);
CREATE INDEX idx_runs_created_at   ON runs(created_at DESC);

CREATE TABLE discrepancies (
    id              BIGSERIAL PRIMARY KEY,
    run_id          TEXT   NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    metric_key      TEXT   NOT NULL,
    metric_category TEXT   NOT NULL,
    block_number    BIGINT NOT NULL,
    subject_type    TEXT   NOT NULL,
    subject_addr    BYTEA,
    values          JSONB  NOT NULL,
    severity        TEXT   NOT NULL,
    trusted_sources TEXT[] NOT NULL,
    reasoning       TEXT   NOT NULL DEFAULT '',
    resolved        BOOLEAN NOT NULL DEFAULT FALSE,
    resolved_at     TIMESTAMPTZ,
    detected_at     TIMESTAMPTZ NOT NULL,

    -- Anchor / Tier / sampling meta (Phase 5 DiffRecord — nullable so
    -- older rows stay valid if Phase 7 starts writing them).
    tier          SMALLINT,
    anchor_block  BIGINT,
    sampling_seed BIGINT
);

CREATE INDEX idx_disc_run            ON discrepancies(run_id);
CREATE INDEX idx_disc_metric_block   ON discrepancies(metric_key, block_number);
CREATE INDEX idx_disc_severity_open  ON discrepancies(severity) WHERE NOT resolved;
CREATE INDEX idx_disc_tier           ON discrepancies(tier) WHERE tier IS NOT NULL;
