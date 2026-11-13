CREATE TABLE outbox_messages (
 id text PRIMARY KEY,
 seq bigserial UNIQUE NOT NULL,
 topic text NOT NULL,
 message_key text NOT NULL,
 payload jsonb NOT NULL,
 trace_context jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 published_at timestamptz,
 attempts integer NOT NULL DEFAULT 0 CHECK(attempts >= 0),
 next_attempt_at timestamptz NOT NULL DEFAULT now(),
 last_error text NOT NULL DEFAULT ''
);
CREATE INDEX outbox_pending ON outbox_messages(seq) WHERE published_at IS NULL;
CREATE INDEX outbox_pending_key ON outbox_messages(message_key,seq) WHERE published_at IS NULL;
