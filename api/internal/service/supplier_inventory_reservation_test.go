package service

import "testing"

func TestSupplierReservationTerminalStatus(t *testing.T) {
	t.Parallel()
	terminal := []string{"cancelled", "expired", "failed", "delivered", "completed", "refunded"}
	for _, status := range terminal {
		if !supplierReservationTerminalStatus(status) {
			t.Errorf("expected %q to release supplier reservations", status)
		}
	}
	nonTerminal := []string{"pending_payment", "pending", "risk_review", "paid", "processing", "refunding", ""}
	for _, status := range nonTerminal {
		if supplierReservationTerminalStatus(status) {
			t.Errorf("did not expect %q to release supplier reservations", status)
		}
	}
}
