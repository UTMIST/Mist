-- Scheduler core (M1 step 3). See docs/agents/architecture/scheduler.md.
-- Times are written by the scheduler from its injected clock, never now(),
-- so tests and the simulator run on virtual time.

CREATE TABLE scheduler_cursors (
    name  text   PRIMARY KEY,
    value bigint NOT NULL
);
-- leader_epoch fences writes from a scheduler that lost leadership;
-- revocation_seq is the last Identity revocation applied.
INSERT INTO scheduler_cursors (name, value) VALUES ('leader_epoch', 0), ('revocation_seq', 0);

CREATE TABLE qos (
    name     text    PRIMARY KEY,
    priority integer NOT NULL, -- higher runs first
    max_time_limit_seconds bigint
);
INSERT INTO qos (name, priority, max_time_limit_seconds) VALUES
    ('normal', 0, NULL),
    ('interactive', 1000, 14400);

CREATE TABLE partitions (
    name        text PRIMARY KEY,
    executor    text NOT NULL,
    state       text NOT NULL DEFAULT 'up' CHECK (state IN ('up', 'down')),
    accelerator text NOT NULL CHECK (accelerator IN ('gpu', 'tt', 'none')),
    runtime     text NOT NULL, -- pinned image digest or host profile
    min_accelerators integer NOT NULL DEFAULT 0,
    max_accelerators integer NOT NULL DEFAULT 0,
    -- Per accelerator (GPU/TT partitions) or per job (accelerator 'none').
    default_milli_cpu    bigint NOT NULL,
    default_memory_bytes bigint NOT NULL,
    max_milli_cpu        bigint NOT NULL,
    max_memory_bytes     bigint NOT NULL,
    default_scratch_bytes bigint NOT NULL,
    max_scratch_bytes     bigint NOT NULL,
    default_time_limit_seconds bigint NOT NULL,
    max_time_limit_seconds     bigint NOT NULL,
    -- Partition-wide caps across all its nodes; NULL means node capacity.
    cap_milli_cpu    bigint,
    cap_memory_bytes bigint
);
-- Proposed initial values from scheduler.md; admins change them in place.
-- The runtime digests are placeholders until the runtime owner pins images.
INSERT INTO partitions VALUES
    ('gpu', 'k3s', 'up', 'gpu', 'unpinned-gpu-runtime', 1, 2,
     8000, 34359738368, 26000, 75161927680,
     107374182400, 214748364800, 3600, 86400, NULL, NULL),
    ('cpu', 'k3s', 'up', 'none', 'unpinned-cpu-runtime', 0, 0,
     2000, 8589934592, 16000, 137438953472,
     53687091200, 536870912000, 3600, 86400, 16000, 206158430208);

CREATE TABLE nodes (
    name        text PRIMARY KEY,
    executor    text NOT NULL,
    admin_state text NOT NULL DEFAULT 'up' CHECK (admin_state IN ('up', 'drain', 'down')),
    -- Last report from Executor.Nodes(), net of executor reservations.
    healthy       boolean NOT NULL DEFAULT false,
    health_detail text    NOT NULL DEFAULT '',
    observed_at   timestamptz,
    milli_cpu       bigint NOT NULL DEFAULT 0,
    memory_bytes    bigint NOT NULL DEFAULT 0,
    gpus            bigint NOT NULL DEFAULT 0,
    tt_boards       bigint NOT NULL DEFAULT 0,
    scratch_bytes   bigint NOT NULL DEFAULT 0,
    hugepages_bytes bigint NOT NULL DEFAULT 0,
    -- Additional Mist host reserve, subtracted once.
    reserve_milli_cpu    bigint NOT NULL DEFAULT 0,
    reserve_memory_bytes bigint NOT NULL DEFAULT 0,
    -- Set when the scheduler cannot prove old operations finished.
    quarantined       boolean NOT NULL DEFAULT false,
    quarantine_detail text    NOT NULL DEFAULT ''
);

CREATE TABLE partition_nodes (
    partition text NOT NULL REFERENCES partitions,
    node      text NOT NULL REFERENCES nodes,
    PRIMARY KEY (partition, node)
);

-- (scope, subject, partition, qos, resource) -> max. See scheduler.md#limits.
CREATE TABLE limits (
    scope     text NOT NULL CHECK (scope IN ('user', 'team', 'partition')),
    subject   text NOT NULL, -- username, team slug or '*'
    partition text NOT NULL, -- name or '*'
    qos       text NOT NULL, -- name or '*'
    resource  text NOT NULL CHECK (resource IN
        ('gpus', 'tt_boards', 'milli_cpu', 'memory_bytes', 'jobs', 'submitted_jobs')),
    max       bigint NOT NULL CHECK (max >= 0),
    PRIMARY KEY (scope, subject, partition, qos, resource)
);
INSERT INTO limits VALUES
    ('user', '*', 'gpu', '*', 'gpus', 1),
    ('team', '*', 'gpu', '*', 'gpus', 2),
    ('user', '*', 'cpu', '*', 'jobs', 4),
    ('user', '*', '*',   '*', 'submitted_jobs', 50);

CREATE TABLE jobs (
    id        bigserial PRIMARY KEY,
    username  text NOT NULL,
    team      text NOT NULL,
    partition text NOT NULL REFERENCES partitions,
    qos       text NOT NULL REFERENCES qos,
    name      text NOT NULL,
    kind      text NOT NULL CHECK (kind IN ('batch', 'command', 'interactive')),
    state     text NOT NULL,
    pending_reason text,
    pending_detail text NOT NULL DEFAULT '',
    held      boolean NOT NULL DEFAULT false,
    -- Requested resources (already normalized).
    milli_cpu     bigint NOT NULL,
    memory_bytes  bigint NOT NULL,
    gpus          bigint NOT NULL,
    tt_boards     bigint NOT NULL,
    scratch_bytes bigint NOT NULL,
    time_limit_seconds   bigint NOT NULL,
    signal_grace_seconds bigint NOT NULL,
    -- Set at admission and never changed.
    node            text REFERENCES nodes,
    execution_id    uuid UNIQUE,
    spec_hash       text,
    auth_generation bigint,
    admitted_at     timestamptz,
    launch_deadline timestamptz,
    -- First committed stop cause wins.
    stop_cause        text,
    stop_actor        text,
    stop_requested_at timestamptz,
    -- Process outcome, separate from state and cleanup.
    exit_code         integer,
    exit_reason       text,
    exit_detail       text NOT NULL DEFAULT '',
    output_incomplete boolean NOT NULL DEFAULT false,
    submitted_at timestamptz NOT NULL,
    started_at   timestamptz,
    finished_at  timestamptz,
    ended_at     timestamptz,
    cleanup_pending boolean NOT NULL DEFAULT false,
    cleanup_detail  text    NOT NULL DEFAULT ''
);
CREATE INDEX jobs_active ON jobs (state)
    WHERE state IN ('PENDING', 'LAUNCHING', 'RUNNING', 'STOPPING', 'CLEANING');
CREATE INDEX jobs_user ON jobs (username, state);

CREATE TABLE job_specs (
    job_id  bigint PRIMARY KEY REFERENCES jobs,
    request jsonb  NOT NULL, -- normalized submission: script, args, argv, env, paths
    launch_spec jsonb        -- frozen LaunchSpec, written at admission
);

CREATE TABLE job_events (
    id         bigserial PRIMARY KEY,
    job_id     bigint NOT NULL REFERENCES jobs,
    at         timestamptz NOT NULL,
    from_state text,
    to_state   text NOT NULL,
    reason     text NOT NULL DEFAULT '',
    actor      text NOT NULL DEFAULT ''
);
CREATE INDEX job_events_job ON job_events (job_id, id);

-- The ledger. An allocation counts against capacity until released_at is
-- set, which only a matching cleanup receipt does.
CREATE TABLE allocations (
    job_id     bigint PRIMARY KEY REFERENCES jobs,
    node       text   NOT NULL REFERENCES nodes,
    partition  text   NOT NULL REFERENCES partitions,
    username   text   NOT NULL,
    team       text   NOT NULL,
    qos        text   NOT NULL,
    milli_cpu       bigint NOT NULL,
    memory_bytes    bigint NOT NULL,
    gpus            bigint NOT NULL,
    tt_boards       bigint NOT NULL,
    scratch_bytes   bigint NOT NULL,
    hugepages_bytes bigint NOT NULL DEFAULT 0,
    device_ids  text[] NOT NULL DEFAULT '{}',
    quarantined boolean NOT NULL DEFAULT false, -- execution lost; held until proven stopped
    created_at  timestamptz NOT NULL,
    released_at timestamptz,
    release_evidence text NOT NULL DEFAULT ''
);
CREATE INDEX allocations_unreleased ON allocations (node) WHERE released_at IS NULL;

-- Committed before every executor call. status 'pending' proves the call
-- was never sent; 'in_flight' is ambiguous until observed.
CREATE TABLE op_intents (
    id           bigserial PRIMARY KEY,
    job_id       bigint NOT NULL REFERENCES jobs,
    execution_id uuid   NOT NULL,
    op           text   NOT NULL CHECK (op IN ('launch', 'terminate')),
    status       text   NOT NULL CHECK (status IN ('pending', 'in_flight', 'done', 'failed')),
    attempts     integer NOT NULL DEFAULT 0,
    last_error   text    NOT NULL DEFAULT '',
    reason       text    NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL,
    updated_at   timestamptz NOT NULL,
    UNIQUE (job_id, op)
);
CREATE INDEX op_intents_open ON op_intents (job_id) WHERE status IN ('pending', 'in_flight');

CREATE TABLE consumed_events (
    event_id    text PRIMARY KEY,
    job_id      bigint NOT NULL,
    consumed_at timestamptz NOT NULL
);
