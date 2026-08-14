package service

import (
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"

	"linlinqi/api/internal/model"
	"linlinqi/api/internal/security"
)

func TestOrderNoFormat(t *testing.T) {
	value := orderNo(time.Date(2026, 8, 9, 12, 30, 45, 0, time.UTC))
	if !regexp.MustCompile(`^LLQ20260809123045\d{6}$`).MatchString(value) {
		t.Fatalf("unexpected order no: %s", value)
	}
}

func TestRevealCompletedOrderRequiresSettledPayment(t *testing.T) {
	vault, err := security.NewVault("completed-order-visibility-test-key-2026")
	if err != nil {
		t.Fatalf("create vault: %v", err)
	}
	productID := uuid.New()
	ciphertext, nonce, _, err := vault.Encrypt("COMPLETED-CARD", productID[:])
	if err != nil {
		t.Fatalf("encrypt card: %v", err)
	}
	order := model.Order{
		Status: "completed", PaymentStatus: "pending",
		Items: []model.OrderItem{{ProductID: productID, CardCiphertext: ciphertext, CardNonce: nonce}},
	}
	if err := revealOrder(vault, &order); err != nil {
		t.Fatalf("inspect unsettled completed order: %v", err)
	}
	if order.Items[0].CardContent != "" {
		t.Fatal("unsettled completed order exposed delivery content")
	}
	order.PaymentStatus = "paid"
	if err := revealOrder(vault, &order); err != nil {
		t.Fatalf("reveal settled completed order: %v", err)
	}
	if order.Items[0].CardContent != "COMPLETED-CARD" {
		t.Fatalf("settled completed order did not reveal delivery: %q", order.Items[0].CardContent)
	}
}
