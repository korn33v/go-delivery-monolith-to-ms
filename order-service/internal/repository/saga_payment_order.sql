-- Existing Sagas already have commands recorded for the payment-first flow.
ALTER TABLE sagas ADD COLUMN payment_first boolean NOT NULL DEFAULT true;
ALTER TABLE sagas ALTER COLUMN payment_first SET DEFAULT false;
