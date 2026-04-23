-- Phase 7I.2 — token stratum coverage for ERC-20 Balance metrics
-- (CapERC20BalanceAtLatest). A Run can carry multiple plans (the
-- Known stratum alone today, with TopNTokens / RandomTokens /
-- FromHoldings queued behind it), so the column is a JSONB array of
-- tagged envelopes: [{"kind": "known_tokens", "data": {...}}, ...] —
-- same shape as runs.address_plans (migration 002).
--
-- NOT NULL with a default empty array keeps rows that predate this
-- migration identical to rows with no token coverage; the mapper
-- treats "[]" and NULL both as "no plans" so the two stay
-- behaviourally equal.

ALTER TABLE runs
    ADD COLUMN token_plans JSONB NOT NULL DEFAULT '[]'::jsonb;
