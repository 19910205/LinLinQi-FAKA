package handler

import "testing"

func TestPaymentDriverConfigChanged(t *testing.T) {
	baseline := paymentDriverConfig{BaseURL: "https://pay.example.com", MerchantID: "merchant", Secret: "secret"}
	if paymentDriverConfigChanged(baseline, baseline) {
		t.Fatal("identical connector config was reported as a rotation")
	}
	for name, changed := range map[string]paymentDriverConfig{
		"endpoint": {BaseURL: "https://pay-2.example.com", MerchantID: baseline.MerchantID, Secret: baseline.Secret},
		"merchant": {BaseURL: baseline.BaseURL, MerchantID: "merchant-2", Secret: baseline.Secret},
		"secret":   {BaseURL: baseline.BaseURL, MerchantID: baseline.MerchantID, Secret: "rotated-secret"},
	} {
		t.Run(name, func(t *testing.T) {
			if !paymentDriverConfigChanged(baseline, changed) {
				t.Fatalf("%s mutation was not detected", name)
			}
		})
	}
}
