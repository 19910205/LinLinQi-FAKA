package service

import (
	"math"
	"testing"
)

func TestRoundedRatioDoesNotOverflowAndRoundsUp(t *testing.T) {
	got, err := roundedRatio(math.MaxInt64, math.MaxInt64-1, math.MaxInt64, true)
	if err != nil {
		t.Fatal(err)
	}
	if got != math.MaxInt64-1 {
		t.Fatalf("got %d", got)
	}
	got, err = roundedRatio(101, 1, 3, true)
	if err != nil || got != 34 {
		t.Fatalf("rounded ratio = %d, %v", got, err)
	}
}

func TestCheckedAddInt64RejectsOverflow(t *testing.T) {
	if _, err := checkedAddInt64(math.MaxInt64, 1); err == nil {
		t.Fatal("expected overflow")
	}
}

func TestCalculateResellerCreditState(t *testing.T) {
	tests := []struct {
		name                   string
		balance, frozen, limit int64
		exposure, remaining    int64
		breached               bool
	}{
		{name: "positive available balance", balance: 1000, frozen: 400, limit: 0, exposure: 0, remaining: 0},
		{name: "credit fully consumed", balance: -100, frozen: 0, limit: 100, exposure: 100, remaining: 0},
		{name: "credit exceeded", balance: -101, frozen: 0, limit: 100, exposure: 101, remaining: 0, breached: true},
		{name: "frozen payout consumes availability", balance: 50, frozen: 100, limit: 70, exposure: 50, remaining: 20},
		{name: "extreme state fails closed", balance: math.MinInt64, frozen: math.MaxInt64, limit: math.MaxInt64, exposure: math.MaxInt64, remaining: 0, breached: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state, err := CalculateResellerCreditState(test.balance, test.frozen, test.limit)
			if err != nil {
				t.Fatal(err)
			}
			if state.Exposure != test.exposure || state.Remaining != test.remaining || state.Breached != test.breached {
				t.Fatalf("unexpected credit state: %#v", state)
			}
		})
	}
	if _, err := CalculateResellerCreditState(0, -1, 0); err == nil {
		t.Fatal("negative frozen balance was accepted")
	}
}

func TestWholesaleSettlementPriceRoundsConservatively(t *testing.T) {
	tests := []struct {
		price    int64
		discount int
		want     int64
	}{
		{price: 10001, discount: 500, want: 9501},
		{price: 100, discount: 0, want: 100},
		{price: 100, discount: 10000, want: 0},
	}
	for _, test := range tests {
		got, err := WholesaleSettlementPrice(test.price, test.discount)
		if err != nil || got != test.want {
			t.Fatalf("settlement(%d, %d) = %d, %v; want %d", test.price, test.discount, got, err, test.want)
		}
	}
	if _, err := WholesaleSettlementPrice(100, 10001); err == nil {
		t.Fatal("out-of-range discount was accepted")
	}
}
