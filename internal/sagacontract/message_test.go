package sagacontract

import (
	"encoding/json"
	"testing"
)

func TestWireContract(t *testing.T) {
	m := New("saga-1", "order-1", ChargePayment, Data{ProductID: 1, Quantity: 1, AmountCents: 100})
	body, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Message
	if err = json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	if err = decoded.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Message){func(m *Message) { m.SchemaVersion = 2 }, func(m *Message) { m.EventID = "" }, func(m *Message) { m.Type = "unknown" }, func(m *Message) { m.Data.Quantity = 0 }, func(m *Message) { m.Data.DemoFailure = "unknown" }} {
		bad := m
		mutate(&bad)
		if bad.Validate() == nil {
			t.Fatal("accepted invalid message", bad)
		}
	}
	if Topic(ChargePayment) != PaymentCommands || Topic(RefundPayment) != PaymentCommands || Topic(ReserveStock) != StockCommands || Topic(PaymentCharged) != Events {
		t.Fatal("incorrect routing")
	}
}
