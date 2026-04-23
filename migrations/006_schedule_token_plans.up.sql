-- Phase 7I.2 — cron-scheduled Runs can now carry TokenSamplingPlans.
-- Mirror of schedules.address_plans (migration 004): without this
-- column, HandleScheduledRun would materialise Runs whose token
-- coverage was dropped between ScheduleRecurring and the DB
-- round-trip. Same shape as runs.token_plans (migration 005) so the
-- serialise helpers Marshal/UnmarshalTokenPlans round-trip both
-- columns.

ALTER TABLE schedules
    ADD COLUMN token_plans JSONB NOT NULL DEFAULT '[]'::jsonb;
