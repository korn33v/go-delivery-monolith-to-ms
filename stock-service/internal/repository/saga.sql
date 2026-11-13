CREATE TABLE products (
 id integer PRIMARY KEY, name text NOT NULL, price_cents bigint NOT NULL CHECK(price_cents>0), available integer NOT NULL CHECK(available>=0)
);
INSERT INTO products VALUES(1,'Ноутбук',9999000,1000000),(2,'Ноутбук (нет в наличии)',9999000,0);
CREATE TABLE stock_receipts (
 order_id text PRIMARY KEY, saga_id text NOT NULL, product_id integer NOT NULL REFERENCES products(id),
 quantity integer NOT NULL CHECK(quantity BETWEEN 1 AND 10), status text NOT NULL CHECK(status IN ('RESERVED','REJECTED')),
 request jsonb NOT NULL, response jsonb NOT NULL, deliveries integer NOT NULL DEFAULT 1
);
