package payment

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

type CreateRequest struct {
	IntentNo  string `json:"intent_no"`
	OrderNo   string `json:"order_no"`
	Amount    int64  `json:"amount"`
	Currency  string `json:"currency"`
	Subject   string `json:"subject"`
	NotifyURL string `json:"notify_url"`
	ReturnURL string `json:"return_url"`
}

type CreateResult struct {
	ProviderTradeNo string    `json:"provider_trade_no"`
	CheckoutURL     string    `json:"checkout_url"`
	QRCode          string    `json:"qr_code"`
	ExpiresAt       time.Time `json:"expires_at"`
}

type QueryResult struct {
	ProviderTradeNo string     `json:"provider_trade_no"`
	Status          string     `json:"status"`
	PaidAmount      int64      `json:"paid_amount"`
	PaidAt          *time.Time `json:"paid_at"`
}

type RefundRequest struct {
	RefundNo        string `json:"refund_no"`
	ProviderTradeNo string `json:"provider_trade_no"`
	Reason          string `json:"reason"`
	Amount          int64  `json:"amount"`
	Currency        string `json:"currency"`
}
type RefundResult struct {
	ProviderRefundNo string `json:"provider_refund_no"`
	Status           string `json:"status"`
}
type CallbackResult struct {
	EventID         string     `json:"event_id"`
	IntentNo        string     `json:"intent_no"`
	ProviderTradeNo string     `json:"provider_trade_no"`
	Status          string     `json:"status"`
	Amount          int64      `json:"amount"`
	Currency        string     `json:"currency"`
	PaidAt          *time.Time `json:"paid_at"`
}

type Driver interface {
	Code() string
	Create(context.Context, CreateRequest) (CreateResult, error)
	Query(context.Context, string) (QueryResult, error)
	Refund(context.Context, RefundRequest) (RefundResult, error)
	VerifyCallback(headers map[string]string, body []byte) (CallbackResult, error)
}

type Registry struct {
	mu      sync.RWMutex
	drivers map[string]Driver
}

func NewRegistry(drivers ...Driver) *Registry {
	r := &Registry{drivers: map[string]Driver{}}
	for _, driver := range drivers {
		r.drivers[driver.Code()] = driver
	}
	return r
}

func (r *Registry) Register(driver Driver) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.drivers[driver.Code()] = driver
}
func (r *Registry) Driver(code string) (Driver, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.drivers[code]
	if !ok {
		return nil, fmt.Errorf("payment driver %q is not registered", code)
	}
	return d, nil
}

var ErrInvalidSignature = errors.New("invalid payment callback signature")
