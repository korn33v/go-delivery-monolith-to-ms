package repository

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"

	"example.com/strangler/internal/messageerror"
	"example.com/strangler/internal/sagacontract"
	"example.com/strangler/order-service/internal/domain"
)

//go:embed saga.sql
var SagaSchema string

type SagaStore struct{ DB *sql.DB }

func (r *SagaStore) Insert(ctx context.Context, tx *sql.Tx, s domain.Saga) error {
	if _, err := tx.ExecContext(ctx, `INSERT INTO orders VALUES($1,$2,$3,$4,$5)`, s.Order.ID, s.Order.ProductID, s.Order.Quantity, s.Order.AmountCents, s.Order.Status); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO sagas VALUES($1,$2,$3,$4,$5)`, s.ID, s.Order.ID, s.State, s.DemoFailure, s.Reason)
	return err
}

func scanSaga(row *sql.Row) (domain.Saga, error) {
	var s domain.Saga
	err := row.Scan(&s.ID, &s.Order.ID, &s.State, &s.DemoFailure, &s.Reason, &s.Order.ProductID, &s.Order.Quantity, &s.Order.AmountCents, &s.Order.Status)
	s.Order.Owner = "order-service"
	if errors.Is(err, sql.ErrNoRows) {
		err = domain.ErrNotFound
	}
	return s, err
}

const selectSaga = `SELECT s.id,s.order_id,s.state,s.demo_failure,s.reason,o.product_id,o.quantity,o.amount_cents,o.status FROM sagas s JOIN orders o ON o.id=s.order_id `

func (r *SagaStore) Lock(ctx context.Context, tx *sql.Tx, id string) (domain.Saga, error) {
	return scanSaga(tx.QueryRowContext(ctx, selectSaga+`WHERE s.id=$1 FOR UPDATE OF s`, id))
}
func (r *SagaStore) Get(ctx context.Context, orderID string) (domain.Saga, error) {
	return scanSaga(r.DB.QueryRowContext(ctx, selectSaga+`WHERE o.id=$1`, orderID))
}
func (r *SagaStore) Save(ctx context.Context, tx *sql.Tx, s domain.Saga, event, next string) error {
	if _, err := tx.ExecContext(ctx, `UPDATE orders SET status=$2 WHERE id=$1`, s.Order.ID, s.Order.Status); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE sagas SET state=$2,reason=$3 WHERE id=$1`, s.ID, s.State, s.Reason); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO saga_history(saga_id,state,message_type,next_command) VALUES($1,$2,$3,$4)`, s.ID, s.State, event, next)
	return err
}

func (r *SagaStore) Receipt(ctx context.Context, tx *sql.Tx, m sagacontract.Message) (*sagacontract.Message, bool, error) {
	var request, response []byte
	err := tx.QueryRowContext(ctx, `SELECT request,response FROM saga_receipts WHERE saga_id=$1 AND message_type=$2`, m.SagaID, m.Type).Scan(&request, &response)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var original sagacontract.Message
	if err = json.Unmarshal(request, &original); err != nil {
		return nil, false, err
	}
	if original.Data != m.Data || original.OrderID != m.OrderID {
		return nil, false, messageerror.Permanent(errors.New("event identity conflict"))
	}
	if _, err = tx.ExecContext(ctx, `UPDATE saga_receipts SET deliveries=deliveries+1 WHERE saga_id=$1 AND message_type=$2`, m.SagaID, m.Type); err != nil {
		return nil, false, err
	}
	if len(response) == 0 {
		return nil, true, nil
	}
	var result sagacontract.Message
	err = json.Unmarshal(response, &result)
	return &result, true, err
}

func (r *SagaStore) Record(ctx context.Context, tx *sql.Tx, m sagacontract.Message, next *sagacontract.Message) error {
	request, err := json.Marshal(m)
	if err != nil {
		return err
	}
	var response any
	if next != nil {
		b, err := json.Marshal(next)
		if err != nil {
			return err
		}
		response = string(b)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO saga_receipts(event_id,saga_id,message_type,request,response) VALUES($1,$2,$3,$4,$5)`, m.EventID, m.SagaID, m.Type, string(request), response)
	return err
}
