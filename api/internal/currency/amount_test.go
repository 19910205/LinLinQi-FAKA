package currency

import "testing"

func TestConvertWithMarkupRoundsOnlyFinalAmount(t *testing.T) {
	amount, err := ConvertWithMarkup(100, 2, 2, "7.0267", 5000)
	if err != nil {
		t.Fatal(err)
	}
	if amount != 1054 {
		t.Fatalf("1 USD at 7.0267 with 50%% markup = %d minor units, want 1054", amount)
	}
	cost, err := Convert(100, 2, 2, "7.0267")
	if err != nil || cost != 703 {
		t.Fatalf("converted cost = %d, %v; want 703", cost, err)
	}
}

func TestConvertSupportsZeroDecimalCurrencies(t *testing.T) {
	amount, err := Convert(1000, 0, 2, "0.0067")
	if err != nil || amount != 670 {
		t.Fatalf("converted amount = %d, %v; want 670", amount, err)
	}
}
