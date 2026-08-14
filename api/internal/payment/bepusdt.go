package payment

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"linlinqi/api/internal/security"
)

// ErrRefundNotSupported is returned by providers that have no refund API. The
// worker treats it as a terminal failure instead of retrying 24 times.
var ErrRefundNotSupported = errors.New("payment provider does not support refunds")

// BepusdtTradeType is a BEpusdt payment method such as usdt.trc20.
type BepusdtTradeType string

// Supported BEpusdt trade types. Kept in sync with v03413/bepusdt v1.24.x
// app/model/order.go so every gateway payment method is configurable.
const (
	BepusdtUsdtTrc20    BepusdtTradeType = "usdt.trc20"
	BepusdtUsdcTrc20    BepusdtTradeType = "usdc.trc20"
	BepusdtUsdtErc20    BepusdtTradeType = "usdt.erc20"
	BepusdtUsdcErc20    BepusdtTradeType = "usdc.erc20"
	BepusdtUsdtBep20    BepusdtTradeType = "usdt.bep20"
	BepusdtUsdcBep20    BepusdtTradeType = "usdc.bep20"
	BepusdtUsdtPolygon  BepusdtTradeType = "usdt.polygon"
	BepusdtUsdcPolygon  BepusdtTradeType = "usdc.polygon"
	BepusdtUsdtArbitrum BepusdtTradeType = "usdt.arbitrum"
	BepusdtUsdcArbitrum BepusdtTradeType = "usdc.arbitrum"
	BepusdtUsdtSolana   BepusdtTradeType = "usdt.solana"
	BepusdtUsdcSolana   BepusdtTradeType = "usdc.solana"
	BepusdtUsdtAptos    BepusdtTradeType = "usdt.aptos"
	BepusdtUsdcAptos    BepusdtTradeType = "usdc.aptos"
	BepusdtUsdtXlayer   BepusdtTradeType = "usdt.xlayer"
	BepusdtUsdcXlayer   BepusdtTradeType = "usdc.xlayer"
	BepusdtUsdcBase     BepusdtTradeType = "usdc.base"
	BepusdtUsdtPlasma   BepusdtTradeType = "usdt.plasma"
	BepusdtUsdtTon      BepusdtTradeType = "usdt.ton"
	BepusdtTronTrx      BepusdtTradeType = "tron.trx"
	BepusdtEthereumEth  BepusdtTradeType = "ethereum.eth"
	BepusdtBscBnb       BepusdtTradeType = "bsc.bnb"
	BepusdtTonGram      BepusdtTradeType = "ton.gram"
)

// BepusdtTradeTypes returns every gateway-supported payment method. Aliases
// keep the naming aligned with BEpusdt's admin UI.
var BepusdtTradeTypes = map[BepusdtTradeType]string{
	BepusdtUsdtTrc20:    "USDT · TRC20",
	BepusdtUsdcTrc20:    "USDC · TRC20",
	BepusdtUsdtErc20:    "USDT · ERC20",
	BepusdtUsdcErc20:    "USDC · ERC20",
	BepusdtUsdtBep20:    "USDT · BEP20",
	BepusdtUsdcBep20:    "USDC · BEP20",
	BepusdtUsdtPolygon:  "USDT · Polygon",
	BepusdtUsdcPolygon:  "USDC · Polygon",
	BepusdtUsdtArbitrum: "USDT · Arbitrum",
	BepusdtUsdcArbitrum: "USDC · Arbitrum",
	BepusdtUsdtSolana:   "USDT · Solana",
	BepusdtUsdcSolana:   "USDC · Solana",
	BepusdtUsdtAptos:    "USDT · Aptos",
	BepusdtUsdcAptos:    "USDC · Aptos",
	BepusdtUsdtXlayer:   "USDT · X Layer",
	BepusdtUsdcXlayer:   "USDC · X Layer",
	BepusdtUsdcBase:     "USDC · Base",
	BepusdtUsdtPlasma:   "USDT · Plasma",
	BepusdtUsdtTon:      "USDT · TON",
	BepusdtTronTrx:      "TRX · Tron",
	BepusdtEthereumEth:  "ETH · Ethereum",
	BepusdtBscBnb:       "BNB · BSC",
	BepusdtTonGram:      "GRAM · TON",
}

// Supported BEpusdt fiat currencies (app/model/registry.go supportFiat).
var BepusdtFiats = map[string]bool{"CNY": true, "USD": true, "EUR": true, "GBP": true, "JPY": true}

func ValidBepusdtTradeType(value string) bool {
	_, ok := BepusdtTradeTypes[BepusdtTradeType(value)]
	return ok
}

type BepusdtConfig struct {
	Code        string
	BaseURL     string
	APIToken    string
	TradeType   string
	Fiat        string
	MinorUnit   int
	Timeout     int
	AllowPrivate bool
}

type BepusdtDriver struct {
	code       string
	baseURL    string
	apiToken   string
	tradeType  string
	fiat       string
	minorUnit  int
	timeout    int
	client     *http.Client
}

func NewBepusdtDriver(cfg BepusdtConfig) *BepusdtDriver {
	timeout := cfg.Timeout
	if timeout < 120 {
		timeout = 900
	}
	return &BepusdtDriver{
		code:      cfg.Code,
		baseURL:   strings.TrimRight(cfg.BaseURL, "/"),
		apiToken:  cfg.APIToken,
		tradeType: cfg.TradeType,
		fiat:      strings.ToUpper(strings.TrimSpace(cfg.Fiat)),
		minorUnit: cfg.MinorUnit,
		timeout:   timeout,
		client:    security.NewOutboundHTTPClient(12*time.Second, cfg.AllowPrivate),
	}
}

func (d *BepusdtDriver) Code() string { return d.code }

// bepusdtSign replicates BEpusdt's EpusdtSign exactly: sort keys except
// signature, skip empty values, join as k=v&..., append the API auth token,
// then MD5 hex.
func bepusdtSign(data map[string]any, token string) string {
	keys := make([]string, 0, len(data))
	for k := range data {
		if k == "signature" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		v := data[k]
		if v == nil || v == "" {
			continue
		}
		b.WriteString(k)
		b.WriteString("=")
		b.WriteString(fmt.Sprintf("%v", v))
		b.WriteString("&")
	}
	sum := md5.Sum([]byte(strings.TrimRight(b.String(), "&") + token))
	return hex.EncodeToString(sum[:])
}

// minorToMajorFloat converts an integer minor-unit amount to the float major
// amount BEpusdt expects. float64 is safe because callback/order amounts are
// bounded far below 2^53 in minor units.
func minorToMajorFloat(amount int64, minorUnit int) float64 {
	return float64(amount) / math.Pow10(minorUnit)
}

func majorToMinorFloat(amount float64, minorUnit int) int64 {
	return int64(math.Round(amount * math.Pow10(minorUnit)))
}

type bepusdtResponse struct {
	StatusCode int             `json:"status_code"`
	Message    string          `json:"message"`
	Data       json.RawMessage `json:"data"`
}

func (d *BepusdtDriver) Create(ctx context.Context, input CreateRequest) (CreateResult, error) {
	req := map[string]any{
		"order_id":     input.IntentNo,
		"amount":       minorToMajorFloat(input.Amount, d.minorUnit),
		"fiat":         d.fiat,
		"trade_type":   d.tradeType,
		"name":         input.Subject,
		"notify_url":   input.NotifyURL,
		"redirect_url": input.ReturnURL,
		"timeout":      d.timeout,
	}
	req["signature"] = bepusdtSign(req, d.apiToken)
	body, err := json.Marshal(req)
	if err != nil {
		return CreateResult{}, fmt.Errorf("encode bepusdt create request: %w", err)
	}
	payload, err := d.call(ctx, http.MethodPost, "/api/v1/order/create-transaction", body)
	if err != nil {
		return CreateResult{}, err
	}
	var data struct {
		TradeID        string `json:"trade_id"`
		Status         int    `json:"status"`
		PaymentURL     string `json:"payment_url"`
		ExpirationTime uint64 `json:"expiration_time"`
	}
	if err := json.Unmarshal(payload, &data); err != nil {
		return CreateResult{}, fmt.Errorf("decode bepusdt create response: %w", err)
	}
	if data.TradeID == "" {
		return CreateResult{}, errors.New("bepusdt create response is missing trade_id")
	}
	expiresAt := time.Now().Add(15 * time.Minute)
	if data.ExpirationTime > 0 {
		expiresAt = time.Now().Add(time.Duration(data.ExpirationTime) * time.Second)
	}
	return CreateResult{ProviderTradeNo: data.TradeID, CheckoutURL: data.PaymentURL, ExpiresAt: expiresAt}, nil
}

func (d *BepusdtDriver) Query(ctx context.Context, tradeNo string) (QueryResult, error) {
	body, err := json.Marshal(map[string]any{"trade_id": tradeNo})
	if err != nil {
		return QueryResult{}, err
	}
	payload, err := d.call(ctx, http.MethodPost, "/api/v1/pay/info", body)
	if err != nil {
		return QueryResult{}, err
	}
	var data struct {
		TradeID string  `json:"trade_id"`
		Status  int     `json:"status"`
		Money   float64 `json:"money"`
	}
	if err := json.Unmarshal(payload, &data); err != nil {
		return QueryResult{}, fmt.Errorf("decode bepusdt query response: %w", err)
	}
	status := "pending"
	switch data.Status {
	case 2:
		status = "succeeded"
	case 5:
		status = "processing"
	case 3, 4, 6:
		status = "failed"
	}
	result := QueryResult{ProviderTradeNo: data.TradeID, Status: status}
	if data.Status == 2 {
		result.PaidAmount = majorToMinorFloat(data.Money, d.minorUnit)
	}
	return result, nil
}

func (d *BepusdtDriver) Refund(_ context.Context, _ RefundRequest) (RefundResult, error) {
	return RefundResult{}, ErrRefundNotSupported
}

type bepusdtCallback struct {
	TradeID            string  `json:"trade_id"`
	OrderID            string  `json:"order_id"`
	Amount             float64 `json:"amount"`
	ActualAmount       string  `json:"actual_amount"`
	Token              string  `json:"token"`
	BlockTransactionID string  `json:"block_transaction_id"`
	Status             int     `json:"status"`
	Signature          string  `json:"signature"`
}

func (d *BepusdtDriver) VerifyCallback(_ map[string]string, body []byte) (CallbackResult, error) {
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return CallbackResult{}, fmt.Errorf("decode bepusdt callback: %w", err)
	}
	provided, _ := raw["signature"].(string)
	if provided == "" || !hmacSafeEqual(bepusdtSign(raw, d.apiToken), provided) {
		return CallbackResult{}, ErrInvalidSignature
	}
	var cb bepusdtCallback
	if err := json.Unmarshal(body, &cb); err != nil {
		return CallbackResult{}, fmt.Errorf("decode bepusdt callback fields: %w", err)
	}
	if cb.Status != 2 {
		return CallbackResult{}, fmt.Errorf("bepusdt callback status %d is not a successful payment", cb.Status)
	}
	if cb.TradeID == "" || cb.OrderID == "" {
		return CallbackResult{}, errors.New("bepusdt callback is missing order identity")
	}
	eventID := cb.BlockTransactionID
	if eventID == "" {
		eventID = "bepusdt:" + cb.TradeID
	}
	return CallbackResult{
		EventID:         eventID,
		IntentNo:        cb.OrderID,
		ProviderTradeNo: cb.TradeID,
		Status:          "succeeded",
		Amount:          majorToMinorFloat(cb.Amount, d.minorUnit),
		Currency:        d.fiat,
	}, nil
}

func hmacSafeEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := range a {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}

func (d *BepusdtDriver) call(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, d.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "LinLinQi/1.0")
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bepusdt request failed: %w", err)
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	var envelope bepusdtResponse
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return nil, fmt.Errorf("bepusdt status %d: invalid response", resp.StatusCode)
	}
	if envelope.StatusCode != 200 {
		message := strings.TrimSpace(envelope.Message)
		if message == "" {
			message = strconv.Itoa(envelope.StatusCode)
		}
		return nil, fmt.Errorf("bepusdt status %d: %s", envelope.StatusCode, message)
	}
	return envelope.Data, nil
}
