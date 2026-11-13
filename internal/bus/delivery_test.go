package bus

import (
	"context"
	"errors"
	"testing"

	"github.com/segmentio/kafka-go"
)

type failurePolicy struct {
	quarantined                    bool
	lookupError, errorAfterSuccess error
}

func (p failurePolicy) IsQuarantined(context.Context, kafka.Message, string) (bool, error) {
	return p.quarantined, p.lookupError
}
func (p failurePolicy) OnSuccess(context.Context, kafka.Message, string) error {
	return p.errorAfterSuccess
}
func (p failurePolicy) OnFailure(context.Context, kafka.Message, string, error) (FailureDecision, error) {
	return FailureDecision{}, nil
}

func TestQuarantinedOffsetNeverRunsHandlerAgain(t *testing.T) {
	calls := 0
	err := handleDelivery(context.Background(), kafka.Message{Value: []byte("rejected")}, "group", func(context.Context, []byte) error { calls++; return nil }, failurePolicy{quarantined: true})
	if err != nil || calls != 0 {
		t.Fatal("quarantine replay caused a business operation", calls, err)
	}
}
func TestUnavailableFailureStorePreventsDeliveryAcknowledgement(t *testing.T) {
	cause := errors.New("database unavailable")
	calls := 0
	err := handleDelivery(context.Background(), kafka.Message{}, "group", func(context.Context, []byte) error { calls++; return nil }, failurePolicy{lookupError: cause})
	if !errors.Is(err, cause) || calls != 0 {
		t.Fatal(calls, err)
	}
	err = handleDelivery(context.Background(), kafka.Message{}, "group", func(context.Context, []byte) error { calls++; return nil }, failurePolicy{errorAfterSuccess: cause})
	if !errors.Is(err, cause) || calls != 1 {
		t.Fatal("acknowledged an unresolved local failure", calls, err)
	}
}
