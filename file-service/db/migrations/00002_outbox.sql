-- +goose Up
CREATE TABLE outbox_events (
    id UUID PRIMARY KEY,
    aggregate_type VARCHAR(100) NOT NULL,
    aggregate_id VARCHAR(100) NOT NULL,
    event_type VARCHAR(100) NOT NULL,
    payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at TIMESTAMPTZ NULL
);

CREATE INDEX outbox_events_unpublished_idx ON outbox_events (created_at) WHERE published_at IS NULL;

-- +goose Down
DROP TABLE outbox_events;
