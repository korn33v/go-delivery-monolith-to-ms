CREATE TABLE orders (
 id text PRIMARY KEY, product_id integer NOT NULL, quantity integer NOT NULL CHECK(quantity BETWEEN 1 AND 10),
 amount_cents bigint NOT NULL CHECK(amount_cents > 0), status text NOT NULL CHECK(status IN ('PENDING','CONFIRMED','CANCELLED'))
);
CREATE TABLE sagas (
 id text PRIMARY KEY, order_id text UNIQUE NOT NULL REFERENCES orders(id),
 state text NOT NULL CHECK(state IN ('PENDING','PAYMENT_PENDING','STOCK_PENDING','COMPENSATING','COMPLETED','CANCELLED','COMPENSATION_FAILED')),
 demo_failure text NOT NULL DEFAULT '', reason text NOT NULL DEFAULT ''
);
CREATE TABLE saga_history (
 seq bigserial PRIMARY KEY, saga_id text NOT NULL REFERENCES sagas(id), state text NOT NULL,
 message_type text NOT NULL, next_command text NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
-- Cached decisions make redelivery idempotent; outgoing intent is stored in Outbox.
CREATE TABLE saga_receipts (
 event_id text PRIMARY KEY, saga_id text NOT NULL REFERENCES sagas(id), message_type text NOT NULL,
 request jsonb NOT NULL, response jsonb, deliveries integer NOT NULL DEFAULT 1, UNIQUE(saga_id, message_type)
);
