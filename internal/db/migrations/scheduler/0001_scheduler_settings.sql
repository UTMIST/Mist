-- Admin-editable scheduler settings (priority weights, cycle timing, launch
-- timeout). Values are validated by the scheduler package before use;
-- changes never alter LaunchSpecs that were already admitted.
CREATE TABLE scheduler_settings (
    key        text        PRIMARY KEY,
    value      jsonb       NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    updated_by text        NOT NULL
);
