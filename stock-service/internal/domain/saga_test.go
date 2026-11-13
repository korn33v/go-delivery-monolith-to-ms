package domain

import "testing"

func TestStockBusinessResults(t *testing.T) {
	p := Product{Available: 2}
	if event, _ := ReserveAvailable(&p, 2); event != "StockReserved" || p.Available != 0 {
		t.Fatal(p)
	}
	if event, reason := ReserveAvailable(&p, 1); event != "StockReservationFailed" || reason != "insufficient_stock" || p.Available != 0 {
		t.Fatal(p)
	}
}
