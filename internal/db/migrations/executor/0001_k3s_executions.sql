-- K3s executor receipts (M1 step 5). Only internal/executor/k3s reads or
-- writes these tables. A row outlives its Pod: deleting a Pod never erases
-- what happened to the execution.
CREATE TABLE k3s_executions (
    execution_id uuid PRIMARY KEY,
    job_id       bigint NOT NULL UNIQUE, -- one execution per job, ever
    spec_hash    text   NOT NULL,
    node         text   NOT NULL,
    create_intent_at timestamptz NOT NULL, -- written before the first create call
    object_uid   text,                     -- Pod UID once observed; forbids recreation
    started_at   timestamptz,
    finished_at  timestamptz,
    result       jsonb,
    log_tail     bytea,
    terminate_requested_at timestamptz,
    lost_at      timestamptz,
    lost_detail  text NOT NULL DEFAULT '',
    pod_deleted_at timestamptz,
    cleaned_at   timestamptz,
    cleanup_evidence text NOT NULL DEFAULT ''
);

CREATE TABLE k3s_receipts (
    id           bigserial PRIMARY KEY,
    execution_id uuid NOT NULL REFERENCES k3s_executions,
    kind         text NOT NULL,
    at           timestamptz NOT NULL,
    object_uid   text NOT NULL DEFAULT '',
    detail       text NOT NULL DEFAULT ''
);
CREATE INDEX k3s_receipts_execution ON k3s_receipts (execution_id, id);
