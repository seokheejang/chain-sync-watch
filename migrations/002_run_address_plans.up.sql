-- Phase 7C.3 — address stratum coverage for AddressLatest / AddressAtBlock
-- metrics. One Run can carry multiple plans (Known + TopN + RecentlyActive
-- stratified mix is the expected shape), so the column is a JSONB array
-- of tagged envelopes: [{"kind": "known", "data": {...}}, ...].
--
-- NOT NULL with a default empty array keeps rows with no address coverage
-- identical to rows written before this migration — the mapper treats "[]"
-- and NULL both as "no plans" so the two cases stay behaviourally equal.

ALTER TABLE runs
    ADD COLUMN address_plans JSONB NOT NULL DEFAULT '[]'::jsonb;
