package payment

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

type SandboxDriver struct{ Secret string }

func (d SandboxDriver) Code() string { return "sandbox" }
func (d SandboxDriver) Create(_ context.Context, req CreateRequest) (CreateResult, error) {
	tradeNo := "SBX-" + req.IntentNo
	return CreateResult{ProviderTradeNo: tradeNo, CheckoutURL: "/sandbox/pay/" + tradeNo, ExpiresAt: time.Now().Add(15 * time.Minute)}, nil
}
func (d SandboxDriver) Query(_ context.Context, tradeNo string) (QueryResult, error) {
	return QueryResult{ProviderTradeNo: tradeNo, Status: "pending"}, nil
}
func (d SandboxDriver) Refund(_ context.Context, req RefundRequest) (RefundResult, error) {
	return RefundResult{ProviderRefundNo: "SBXR-" + req.RefundNo, Status: "succeeded"}, nil
}
func (d SandboxDriver) VerifyCallback(headers map[string]string, body []byte) (CallbackResult, error) {
	signature := headers["X-Signature"]
	mac := hmac.New(sha256.New, []byte(d.Secret))
	mac.Write(body)
	if !hmac.Equal([]byte(hex.EncodeToString(mac.Sum(nil))), []byte(signature)) {
		return CallbackResult{}, ErrInvalidSignature
	}
	var result CallbackResult
	if err := json.Unmarshal(body, &result); err != nil {
		return CallbackResult{}, fmt.Errorf("decode callback: %w", err)
	}
	return result, nil
}
