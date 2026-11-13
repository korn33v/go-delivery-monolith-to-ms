package messageerror

import (
	"errors"
	"fmt"
	"testing"
)

func TestErrorClassificationPreservesCause(t *testing.T) {
	cause := errors.New("invalid transition")
	if IsPermanent(cause) {
		t.Fatal("technical errors must be retried")
	}
	err := fmt.Errorf("handler: %w", Permanent(cause))
	if !IsPermanent(err) || !errors.Is(err, cause) {
		t.Fatal("classification lost original business error")
	}
	if Permanent(nil) != nil {
		t.Fatal("nil must remain success")
	}
}
