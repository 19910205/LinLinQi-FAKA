package service

import (
	"testing"

	"github.com/google/uuid"

	"linlinqi/api/internal/model"
)

func TestRefundProviderCapacityRejectsInvalidNormalIntentBeforeDatabaseAccess(t *testing.T) {
	if _, err := RefundProviderCapacityTx(nil, model.PaymentIntent{Base: model.Base{ID: uuid.New()}}, "admin:test", "CNY"); err == nil {
		t.Fatal("zero intent amount was accepted")
	}
}
