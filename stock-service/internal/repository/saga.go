package repository

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"time"

	"example.com/strangler/internal/messageerror"
	"example.com/strangler/internal/sagacontract"
	"example.com/strangler/stock-service/internal/domain"
)

//go:embed saga.sql
var SagaSchema string

//go:embed saga_release.sql
var ReleaseSchema string

type SagaStore struct{ DB *sql.DB }

func (r *SagaStore) Lock(ctx context.Context, tx *sql.Tx, id int) (domain.Product, error) {
	var p domain.Product
	err := tx.QueryRowContext(ctx, `SELECT id,name,price_cents,available FROM products WHERE id=$1 FOR UPDATE`, id).Scan(&p.ID, &p.Name, &p.PriceCents, &p.Available)
	if errors.Is(err, sql.ErrNoRows) {
		err = domain.ErrNotFound
	}
	return p, err
}
func (r *SagaStore) Result(ctx context.Context, tx *sql.Tx, m sagacontract.Message) (sagacontract.Message, bool, error) {
	var request, response []byte
	err := tx.QueryRowContext(ctx, `SELECT request,response FROM stock_receipts WHERE order_id=$1`, m.OrderID).Scan(&request, &response)
	if errors.Is(err, sql.ErrNoRows) {
		return sagacontract.Message{}, false, nil
	}
	if err != nil {
		return sagacontract.Message{}, false, err
	}
	var original, event sagacontract.Message
	if err = json.Unmarshal(request, &original); err != nil {
		return event, false, err
	}
	if original.SagaID != m.SagaID || original.Data != m.Data {
		return event, false, messageerror.Permanent(errors.New("stock idempotency key conflict"))
	}
	if _, err = tx.ExecContext(ctx, `UPDATE stock_receipts SET deliveries=deliveries+1 WHERE order_id=$1`, m.OrderID); err != nil {
		return event, false, err
	}
	err = json.Unmarshal(response, &event)
	return event, true, err
}
func (r *SagaStore) Save(ctx context.Context, tx *sql.Tx, p domain.Product, m, event sagacontract.Message) error {
	if _, err := tx.ExecContext(ctx, `UPDATE products SET available=$2 WHERE id=$1`, p.ID, p.Available); err != nil {
		return err
	}
	status := "REJECTED"
	if event.Type == sagacontract.StockReserved {
		status = "RESERVED"
	}
	request, err := json.Marshal(m)
	if err != nil {
		return err
	}
	response, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO stock_receipts(order_id,saga_id,product_id,quantity,status,request,response) VALUES($1,$2,$3,$4,$5,$6,$7)`, m.OrderID, m.SagaID, p.ID, m.Data.Quantity, status, string(request), string(response))
	return err
}

func (r *SagaStore) Release(ctx context.Context, tx *sql.Tx, p domain.Product, m sagacontract.Message) (sagacontract.Message, bool, error) {
	var request, response []byte
	var status string
	err := tx.QueryRowContext(ctx, `SELECT request,release_response,status FROM stock_receipts WHERE order_id=$1`, m.OrderID).Scan(&request, &response, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return sagacontract.Message{}, false, messageerror.Permanent(errors.New("stock reservation not found"))
	}
	if err != nil {
		return sagacontract.Message{}, false, err
	}
	var original sagacontract.Message
	if err := json.Unmarshal(request, &original); err != nil {
		return sagacontract.Message{}, false, err
	}
	if original.Type != sagacontract.ReserveStock || original.SagaID != m.SagaID || original.Data != m.Data || original.Data.ProductID != p.ID {
		return sagacontract.Message{}, false, messageerror.Permanent(errors.New("stock reservation does not match release"))
	}
	if status == "RELEASED" {
		var event sagacontract.Message
		if err := json.Unmarshal(response, &event); err != nil {
			return event, false, err
		}
		_, err := tx.ExecContext(ctx, `UPDATE stock_receipts SET deliveries=deliveries+1 WHERE order_id=$1`, m.OrderID)
		return event, true, err
	}
	if status != "RESERVED" {
		return sagacontract.Message{}, false, messageerror.Permanent(errors.New("stock reservation is not active"))
	}
	event := sagacontract.Reply(m, sagacontract.StockReleased, "")
	encoded, err := json.Marshal(event)
	if err != nil {
		return event, false, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE products SET available=available+$2 WHERE id=$1`, p.ID, m.Data.Quantity); err != nil {
		return event, false, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE stock_receipts SET status='RELEASED',release_response=$2 WHERE order_id=$1`, m.OrderID, string(encoded))
	return event, false, err
}

// Keep the existing Stock HTTP API usable in Saga mode too.
func (r *SagaStore) Get(id int) (domain.Product, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var p domain.Product
	err := r.DB.QueryRowContext(ctx, `SELECT id,name,price_cents,available FROM products WHERE id=$1`, id).Scan(&p.ID, &p.Name, &p.PriceCents, &p.Available)
	if errors.Is(err, sql.ErrNoRows) {
		err = domain.ErrNotFound
	}
	return p, err
}
func (r *SagaStore) Reserve(request domain.Reservation) (domain.Product, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return domain.Product{}, err
	}
	defer tx.Rollback()
	p, err := r.Lock(ctx, tx, request.ProductID)
	if err != nil {
		return p, err
	}
	m := sagacontract.New("legacy-"+request.OrderID, request.OrderID, sagacontract.ReserveStock, sagacontract.Data{ProductID: request.ProductID, Quantity: request.Quantity, AmountCents: p.PriceCents * request.Quantity})
	result, duplicate, err := r.Result(ctx, tx, m)
	if err != nil {
		return p, domain.ErrConflict
	}
	if !duplicate {
		kind, reason := domain.ReserveAvailable(&p, request.Quantity)
		result = sagacontract.Reply(m, kind, reason)
		if err = r.Save(ctx, tx, p, m, result); err != nil {
			return p, err
		}
	}
	if err = tx.Commit(); err != nil {
		return p, err
	}
	if result.Type == sagacontract.StockReservationFailed {
		return p, domain.ErrConflict
	}
	return p, nil
}
