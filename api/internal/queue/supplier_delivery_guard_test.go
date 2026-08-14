package queue

import (
	"testing"

	"linlinqi/api/internal/model"
)

func TestSupplierOrderDeliverableRequiresPaidProcessingState(t *testing.T) {
	if !supplierOrderDeliverable(model.Order{Status: "processing", PaymentStatus: "paid"}) {
		t.Fatal("paid processing order should be deliverable")
	}
	for _, order := range []model.Order{
		{Status: "refunding", PaymentStatus: "paid"},
		{Status: "refunded", PaymentStatus: "refunded"},
		{Status: "processing", PaymentStatus: "partially_refunded"},
		{Status: "failed", PaymentStatus: "paid"},
		{Status: "delivered", PaymentStatus: "paid"},
	} {
		if supplierOrderDeliverable(order) {
			t.Fatalf("non-deliverable order was accepted: status=%s payment=%s", order.Status, order.PaymentStatus)
		}
	}
}
