package stockclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"example.com/strangler/internal/contract"
)

type HTTPError struct {
	Status  int
	Message string
}

func (e *HTTPError) Error() string { return e.Message }

type Client struct {
	URL  string
	HTTP *http.Client
}

func (c *Client) Product(ctx context.Context, id int) (contract.Product, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/api/v1/stock/%d", c.URL, id), nil)
	if err != nil {
		return contract.Product{}, err
	}
	response, err := c.HTTP.Do(req)
	if err != nil {
		return contract.Product{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return contract.Product{}, &HTTPError{response.StatusCode, "stock read failed"}
	}
	var product contract.Product
	err = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&product)
	return product, err
}

// Reserve atomically checks and subtracts stock. A preceding GET alone would
// race with other orders. order_id is the idempotency key of the reservation.
func (c *Client) Reserve(ctx context.Context, request contract.Reservation) (contract.Product, error) {
	body, err := json.Marshal(request)
	if err != nil {
		return contract.Product{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL+"/api/v1/stock/reservations", bytes.NewReader(body))
	if err != nil {
		return contract.Product{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := c.HTTP.Do(req)
	if err != nil {
		return contract.Product{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return contract.Product{}, &HTTPError{response.StatusCode, "stock reservation failed"}
	}
	var product contract.Product
	err = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&product)
	return product, err
}
