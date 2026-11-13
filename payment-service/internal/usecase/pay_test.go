package usecase

import (
	"context"
	"example.com/strangler/payment-service/internal/domain"
	"example.com/strangler/payment-service/internal/repository"
	"testing"
)

type fakePublisher struct {
	calls int
	fail  bool
}

func (p *fakePublisher) Completed(context.Context, domain.Result) error {
	p.calls++
	if p.fail {
		return context.DeadlineExceeded
	}
	return nil
}

type observer struct{ charges int }

func (o *observer) Event(string, string) { o.charges++ }

func TestRetryAfterPublishFailureDoesNotChargeTwice(t *testing.T) {
	results := repository.New()
	publisher := &fakePublisher{fail: true}
	metrics := &observer{}
	service := New(results, publisher, metrics, 0)
	order := domain.Order{ID: "order-1", Owner: "order-service", AmountCents: 100, Quantity: 1}
	if err := service.Pay(context.Background(), order); err == nil {
		t.Fatal("expected failed publication")
	}
	publisher.fail = false
	if err := service.Pay(context.Background(), order); err != nil {
		t.Fatal(err)
	}
	if metrics.charges != 1 || publisher.calls != 2 {
		t.Fatalf("charges=%d results=%d", metrics.charges, publisher.calls)
	}
}
