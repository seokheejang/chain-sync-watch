-- Phase 7E — durable schedule store. Replaces the in-memory
-- scheduleStore that Phase 7A used to prototype the cron path.
-- Dispatcher.ScheduleRecurring writes here; the DB-backed
-- PeriodicTaskConfigProvider reads ListActive() on every sync tick.
--
-- active=false soft-deletes a schedule (cancelled by operator,
-- replaced by a newer version, etc.) without losing the audit trail.

CREATE TABLE schedules (
    job_id        TEXT PRIMARY KEY,
    chain_id      BIGINT NOT NULL,
    cron_expr     TEXT   NOT NULL,
    timezone      TEXT   NOT NULL DEFAULT 'UTC',
    strategy_kind TEXT   NOT NULL,
    strategy_data JSONB  NOT NULL,
    metrics       TEXT[] NOT NULL,
    active        BOOLEAN NOT NULL DEFAULT TRUE,
    created_at    TIMESTAMPTZ NOT NULL
);

-- The provider polls every SyncInterval (default 10s) and only needs
-- active rows. A partial index keeps that scan tight even if the
-- table accumulates thousands of cancelled schedules over time.
CREATE INDEX idx_schedules_active ON schedules(active) WHERE active;
