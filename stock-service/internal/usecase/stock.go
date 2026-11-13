package usecase

import "example.com/strangler/stock-service/internal/domain"

type Service struct{ Store domain.Store }

func New(store domain.Store) *Service                 { return &Service{Store: store} }
func (s *Service) Get(id int) (domain.Product, error) { return s.Store.Get(id) }
func (s *Service) Reserve(request domain.Reservation) (domain.Product, error) {
	if request.ProductID < 1 || request.Quantity < 1 || request.Quantity > 10 || request.OrderID == "" || len(request.OrderID) > 100 {
		return domain.Product{}, domain.ErrInvalid
	}
	return s.Store.Reserve(request)
}
