-- Phase 7F — cron-scheduled Runs can now carry AddressSamplingPlans.
-- Without this column, HandleScheduledRun materialised Runs with
-- zero address coverage even though the Dispatcher API accepted
-- plans on the SchedulePayload — the plans got dropped between
-- ScheduleRecurring and the DB round-trip. Same shape as
-- runs.address_plans (migration 002) so the serialise helpers
-- Marshal/UnmarshalAddressPlans round-trip both columns.

ALTER TABLE schedules
    ADD COLUMN address_plans JSONB NOT NULL DEFAULT '[]'::jsonb;
