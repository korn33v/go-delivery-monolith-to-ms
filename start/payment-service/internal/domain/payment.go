package domain

import "context"

type Order struct {
	ID          string
	Owner       string
	AmountCents int
	Quantity    int
}
type Result struct {
	EventID string
	OrderID string
	Owner   string
	Status  string
}
type Results interface {
	Get(string) (Result, bool)
	Save(string, Result)
}
type Publisher interface {
	Completed(context.Context, Result) error
}
type Observer interface{ Event(string, string) }
