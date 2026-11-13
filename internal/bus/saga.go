package bus

import (
	"context"
	"log/slog"

	"example.com/strangler/internal/sagacontract"
)

// Send is shared transport plumbing; orchestration stays in Order's use case.
func (p *Producer) Send(ctx context.Context, m sagacontract.Message) error {
	if err := m.Validate(); err != nil {
		return err
	}
	err := p.Publish(ctx, sagacontract.Topic(m.Type), m.SagaID, m)
	slog.Info("saga publish", "service", p.Service, "order_id", m.OrderID, "saga_id", m.SagaID, "message_type", m.Type, "saga_state", "", "error", err)
	return err
}
