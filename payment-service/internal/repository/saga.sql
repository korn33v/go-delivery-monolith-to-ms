CREATE TABLE payments (
 order_id text PRIMARY KEY, saga_id text UNIQUE NOT NULL, amount_cents bigint NOT NULL CHECK(amount_cents>0),
 product_id integer NOT NULL, quantity integer NOT NULL, demo_failure text NOT NULL,
 state text NOT NULL CHECK(state IN ('NEW','CHARGED','DECLINED','REFUNDED','REFUND_FAILED')),
 charges integer NOT NULL DEFAULT 0 CHECK(charges BETWEEN 0 AND 1),
 refunds integer NOT NULL DEFAULT 0 CHECK(refunds BETWEEN 0 AND charges)
);
CREATE TABLE payment_receipts (
 order_id text NOT NULL REFERENCES payments(order_id), message_type text NOT NULL,
 request jsonb NOT NULL, response jsonb NOT NULL, deliveries integer NOT NULL DEFAULT 1, PRIMARY KEY(order_id,message_type)
);
