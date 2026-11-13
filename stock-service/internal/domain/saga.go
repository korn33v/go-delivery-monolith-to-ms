package domain

// ReserveAvailable is a local business decision; the repository locks the row.
func ReserveAvailable(product *Product, quantity int) (string, string) {
	if product.Available < quantity {
		return "StockReservationFailed", "insufficient_stock"
	}
	product.Available -= quantity
	return "StockReserved", ""
}
