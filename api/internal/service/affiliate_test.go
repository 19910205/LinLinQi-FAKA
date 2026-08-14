package service

import "testing"

func TestAffiliateReversalTarget(t *testing.T) {
	for name, test := range map[string]struct {
		commission, refunded, total, want int64
	}{
		"zero":              {100, 0, 1000, 0},
		"half exact":        {100, 500, 1000, 50},
		"partial rounds up": {101, 1, 1000, 1},
		"full":              {101, 1000, 1000, 101},
		"over refund caps":  {101, 1200, 1000, 101},
	} {
		t.Run(name, func(t *testing.T) {
			if got := affiliateReversalTarget(test.commission, test.refunded, test.total); got != test.want {
				t.Fatalf("reversal target = %d, want %d", got, test.want)
			}
		})
	}
}
