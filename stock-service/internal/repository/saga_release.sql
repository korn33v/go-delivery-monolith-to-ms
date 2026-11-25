ALTER TABLE stock_receipts DROP CONSTRAINT stock_receipts_status_check;
ALTER TABLE stock_receipts ADD CONSTRAINT stock_receipts_status_check
    CHECK (status IN ('RESERVED', 'REJECTED', 'RELEASED'));
ALTER TABLE stock_receipts ADD COLUMN release_response jsonb;
