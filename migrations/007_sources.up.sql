-- Phase 10a — DB-backed source configuration store.
-- Each row describes one adapter instance (rpc / blockscout / routescan
-- / etherscan / user-defined indexer) wired to a specific chain. The
-- bundled default endpoints (config/defaults.yaml) seed this table
-- on first boot via `csw migrate seed`; after that the table is the
-- single source of truth. YAML becomes replay-for-reset only.
--
-- secret_ciphertext + secret_nonce hold an AES-GCM payload keyed off
-- CSW_SECRET_KEY. The DB never sees the plaintext api_key. Columns
-- are nullable because the RPC / Blockscout / Routescan adapters need
-- no credentials in the default configuration.
--
-- UNIQUE(type, chain_id) caps each adapter type at one enabled row
-- per chain. Multi-instance redundancy (two RPCs for failover) is a
-- post-10a follow-up — requires adapters to accept a SourceID option
-- rather than exposing a package-level const.

CREATE TABLE sources (
    id                TEXT        PRIMARY KEY,
    chain_id          BIGINT      NOT NULL,
    type              TEXT        NOT NULL,
    endpoint          TEXT        NOT NULL,
    secret_ciphertext BYTEA,
    secret_nonce      BYTEA,
    options           JSONB       NOT NULL DEFAULT '{}'::jsonb,
    enabled           BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT sources_secret_pair CHECK (
        (secret_ciphertext IS NULL AND secret_nonce IS NULL)
        OR (secret_ciphertext IS NOT NULL AND secret_nonce IS NOT NULL)
    ),
    CONSTRAINT sources_type_chain_unique UNIQUE (type, chain_id)
);

CREATE INDEX idx_sources_chain_enabled ON sources (chain_id, enabled);
