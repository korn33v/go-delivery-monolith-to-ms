package usecase

import (
	"context"
	"example.com/strangler/monolith/internal/domain"
	"example.com/strangler/monolith/internal/repository"
	"testing"
)

type settings struct{ mode domain.Mode }

func (s settings) Mode() (domain.Mode, error) { return s.mode, nil }

type stock struct{ calls int }

func (s *stock) Reserve(context.Context, string, int, int) (domain.Product, error) {
	s.calls++
	return domain.Product{}, nil
}

type publisher struct{ calls int }

func (p *publisher) Created(context.Context, domain.Order) error { p.calls++; return nil }

type payment struct{ calls int }

func (p *payment) Pay(context.Context, domain.Order) error { p.calls++; return nil }

type observer struct{}

func (observer) Event(string, string) {}

func TestMigrationKeepsLocalStockAndSwitchesPayment(t *testing.T) {
	for _, async := range []bool{false, true} {
		orders := repository.NewOrders()
		local := repository.NewStock(3)
		remote := &stock{}
		pub := &publisher{}
		pay := &payment{}
		svc := New(orders, local, remote, pub, pay, settings{domain.Mode{Async: async, Bridge: true}}, observer{})
		order, err := svc.Create(context.Background(), domain.CreateOrder{ProductID: 1, Quantity: 1})
		if err != nil {
			t.Fatal(err)
		}
		product, _ := local.Get(1)
		if product.Available != 2 || remote.calls != 1 {
			t.Fatal("local stock or migration bridge was bypassed")
		}
		if async && (pub.calls != 1 || pay.calls != 0 || order.Status != "NEW") {
			t.Fatal("async mode called local payment")
		}
		if !async && (pub.calls != 0 || pay.calls != 1 || order.Status != "PAID") {
			t.Fatal("local mode published to Kafka")
		}
	}
}
