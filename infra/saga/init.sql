-- One PostgreSQL server saves laptop resources; three isolated databases/users.
CREATE ROLE orders LOGIN PASSWORD 'orders_demo';
CREATE ROLE payments LOGIN PASSWORD 'payments_demo';
CREATE ROLE stock LOGIN PASSWORD 'stock_demo';
CREATE DATABASE orders OWNER orders;
CREATE DATABASE payments OWNER payments;
CREATE DATABASE stock OWNER stock;
REVOKE CONNECT ON DATABASE orders FROM PUBLIC;
REVOKE CONNECT ON DATABASE payments FROM PUBLIC;
REVOKE CONNECT ON DATABASE stock FROM PUBLIC;
GRANT CONNECT ON DATABASE orders TO orders;
GRANT CONNECT ON DATABASE payments TO payments;
GRANT CONNECT ON DATABASE stock TO stock;
