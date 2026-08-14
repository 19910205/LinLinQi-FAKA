package payment

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"linlinqi/api/internal/security"
)

// SignedHTTPDriver implements LinLinQi's independent provider-neutral payment protocol.
type SignedHTTPDriver struct {
	code, baseURL, merchantID, secret string
	client                            *http.Client
}

func NewSignedHTTPDriver(code, baseURL, merchantID, secret string, allowPrivate bool) *SignedHTTPDriver {
	return &SignedHTTPDriver{code: code, baseURL: strings.TrimRight(baseURL, "/"), merchantID: merchantID, secret: secret, client: security.NewOutboundHTTPClient(12*time.Second, allowPrivate)}
}
func (d *SignedHTTPDriver) Code() string { return d.code }
func (d *SignedHTTPDriver) Create(ctx context.Context, input CreateRequest) (CreateResult, error) {
	var out CreateResult
	err := d.call(ctx, http.MethodPost, "/v1/payments", input, &out)
	return out, err
}
func (d *SignedHTTPDriver) Query(ctx context.Context, tradeNo string) (QueryResult, error) {
	var out QueryResult
	err := d.call(ctx, http.MethodGet, "/v1/payments/"+url.PathEscape(tradeNo), nil, &out)
	return out, err
}
func (d *SignedHTTPDriver) Refund(ctx context.Context, input RefundRequest) (RefundResult, error) {
	var out RefundResult
	err := d.call(ctx, http.MethodPost, "/v1/refunds", input, &out)
	return out, err
}
func (d *SignedHTTPDriver) VerifyCallback(headers map[string]string, body []byte) (CallbackResult, error) {
	timestamp, err := strconv.ParseInt(headers["X-Timestamp"], 10, 64)
	if err != nil || time.Since(time.Unix(timestamp, 0)) > 5*time.Minute || time.Until(time.Unix(timestamp, 0)) > time.Minute {
		return CallbackResult{}, ErrInvalidSignature
	}
	expected := sign(d.secret, headers["X-Timestamp"]+"."+string(body))
	if !hmac.Equal([]byte(expected), []byte(headers["X-Signature"])) {
		return CallbackResult{}, ErrInvalidSignature
	}
	var out CallbackResult
	err = json.Unmarshal(body, &out)
	return out, err
}
func (d *SignedHTTPDriver) call(ctx context.Context, method, path string, input, out any) error {
	var body []byte
	var err error
	if input != nil {
		body, err = json.Marshal(input)
		if err != nil {
			return err
		}
	}
	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	signature := sign(d.secret, timestamp+"."+method+"."+path+"."+hexSHA(body))
	req, err := http.NewRequestWithContext(ctx, method, d.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Merchant-ID", d.merchantID)
	req.Header.Set("X-Timestamp", timestamp)
	req.Header.Set("X-Signature", signature)
	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("provider status %d: %s", resp.StatusCode, string(payload))
	}
	return json.Unmarshal(payload, out)
}
func sign(secret, value string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(value))
	return hex.EncodeToString(mac.Sum(nil))
}
func hexSHA(value []byte) string { sum := sha256.Sum256(value); return hex.EncodeToString(sum[:]) }
