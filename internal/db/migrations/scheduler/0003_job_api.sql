-- Job API (M1 step 6). See docs/agents/architecture/api.md.

-- Submission idempotency. The hash is of the client's request as sent, not
-- the normalized spec, so a retry gets the original job even if defaults
-- changed in between. Kept as long as the job row.
CREATE TABLE submissions (
    username        text   NOT NULL,
    idempotency_key text   NOT NULL,
    request_hash    text   NOT NULL,
    job_id          bigint NOT NULL REFERENCES jobs,
    created_at      timestamptz NOT NULL,
    PRIMARY KEY (username, idempotency_key)
);

-- Defaults the API fills in. Defaults live only here, never in clients.
ALTER TABLE partitions
    ADD COLUMN is_default boolean NOT NULL DEFAULT false,
    ADD COLUMN default_signal_grace_seconds bigint NOT NULL DEFAULT 60
        CHECK (default_signal_grace_seconds BETWEEN 1 AND 120);
CREATE UNIQUE INDEX partitions_one_default ON partitions (is_default) WHERE is_default;
UPDATE partitions SET is_default = true WHERE name = 'gpu';
