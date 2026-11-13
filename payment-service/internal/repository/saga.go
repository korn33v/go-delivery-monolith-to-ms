package repository

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"

	"example.com/strangler/internal/messageerror"
	"example.com/strangler/internal/sagacontract"
	"example.com/strangler/payment-service/internal/domain"
)

//go:embed saga.sql
var SagaSchema string

type SagaStore struct{ DB *sql.DB }

func (r *SagaStore) Lock(ctx context.Context, tx *sql.Tx, m sagacontract.Message) (domain.SagaPayment, error) {
	if m.Type == sagacontract.ChargePayment {
		_, err := tx.ExecContext(ctx, `INSERT INTO payments(order_id,saga_id,amount_cents,product_id,quantity,demo_failure,state) VALUES($1,$2,$3,$4,$5,$6,'NEW') ON CONFLICT(order_id) DO NOTHING`, m.OrderID, m.SagaID, m.Data.AmountCents, m.Data.ProductID, m.Data.Quantity, m.Data.DemoFailure)
		if err != nil {
			return domain.SagaPayment{}, err
		}
	}
	var p domain.SagaPayment
	err := tx.QueryRowContext(ctx, `SELECT order_id,saga_id,amount_cents,product_id,quantity,demo_failure,state,charges,refunds FROM payments WHERE order_id=$1 FOR UPDATE`, m.OrderID).Scan(&p.OrderID, &p.SagaID, &p.AmountCents, &p.ProductID, &p.Quantity, &p.DemoFailure, &p.State, &p.Charges, &p.Refunds)
	if err != nil {
		return p, err
	}
	if p.SagaID != m.SagaID || p.AmountCents != m.Data.AmountCents || p.ProductID != m.Data.ProductID || p.Quantity != m.Data.Quantity || p.DemoFailure != m.Data.DemoFailure {
		return p, messageerror.Permanent(errors.New("payment idempotency key conflict"))
	}
	return p, nil
}
func (r *SagaStore) Result(ctx context.Context, tx *sql.Tx, m sagacontract.Message) (sagacontract.Message, bool, error) {
	var body []byte
	err := tx.QueryRowContext(ctx, `SELECT response FROM payment_receipts WHERE order_id=$1 AND message_type=$2`, m.OrderID, m.Type).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return sagacontract.Message{}, false, nil
	}
	if err != nil {
		return sagacontract.Message{}, false, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE payment_receipts SET deliveries=deliveries+1 WHERE order_id=$1 AND message_type=$2`, m.OrderID, m.Type); err != nil {
		return sagacontract.Message{}, false, err
	}
	var event sagacontract.Message
	err = json.Unmarshal(body, &event)
	return event, true, err
}
func (r *SagaStore) Save(ctx context.Context, tx *sql.Tx, p domain.SagaPayment, m, event sagacontract.Message) error {
	_, err := tx.ExecContext(ctx, `UPDATE payments SET state=$2,charges=$3,refunds=$4 WHERE order_id=$1`, p.OrderID, p.State, p.Charges, p.Refunds)
	if err != nil {
		return err
	}
	request, err := json.Marshal(m)
	if err != nil {
		return err
	}
	response, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO payment_receipts(order_id,message_type,request,response) VALUES($1,$2,$3,$4)`, p.OrderID, m.Type, string(request), string(response))
	return err
}
