package handler

import "testing"

func TestProportionalPaymentAmountUsesExactIntegerRounding(t *testing.T) {
	tests := []struct {
		name         string
		orderPart    int64
		orderTotal   int64
		paymentTotal int64
		want         int64
	}{
		{name: "same currency", orderPart: 250, orderTotal: 1000, paymentTotal: 1000, want: 250},
		{name: "cross currency half up", orderPart: 1, orderTotal: 3, paymentTotal: 100, want: 33},
		{name: "USD to CNY", orderPart: 100, orderTotal: 100, paymentTotal: 702, want: 702},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := proportionalPaymentAmount(test.orderPart, test.orderTotal, test.paymentTotal)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("proportionalPaymentAmount() = %d, want %d", got, test.want)
			}
		})
	}
}

func TestProportionalPaymentAmountRejectsInvalidChain(t *testing.T) {
	for _, values := range [][3]int64{{0, 100, 100}, {1, 0, 100}, {101, 100, 100}, {1, 100, 0}} {
		if _, err := proportionalPaymentAmount(values[0], values[1], values[2]); err == nil {
			t.Fatalf("expected invalid chain error for %#v", values)
		}
	}
}

func TestIncrementalProportionalPaymentAmountUsesCumulativeRounding(t *testing.T) {
	first, err := incrementalProportionalPaymentAmount(0, 1, 3, 0, 2)
	if err != nil || first != 1 {
		t.Fatalf("first allocation = %d, err=%v", first, err)
	}
	if _, err := incrementalProportionalPaymentAmount(1, 1, 3, 1, 2); err == nil {
		t.Fatal("sub-minor-unit second allocation should require a larger order-side refund")
	}
	remainder, err := incrementalProportionalPaymentAmount(1, 2, 3, 1, 2)
	if err != nil || remainder != 1 {
		t.Fatalf("remainder allocation = %d, err=%v", remainder, err)
	}
	partial, err := incrementalProportionalPaymentAmount(0, 2, 3, 0, 2)
	if err != nil || partial != 1 {
		t.Fatalf("two-unit partial allocation = %d, err=%v", partial, err)
	}
	final, err := incrementalProportionalPaymentAmount(2, 1, 3, 1, 2)
	if err != nil || final != 1 {
		t.Fatalf("final allocation = %d, err=%v", final, err)
	}
}
