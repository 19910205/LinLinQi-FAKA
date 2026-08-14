package currency

import (
	"testing"

	"linlinqi/api/internal/model"
)

func TestPrioritizeProviderDriversPreventsAggregatorStarvation(t *testing.T) {
	configs := []model.FXProviderConfig{
		{Code: "a", Driver: "frankfurter-v2"},
		{Code: "b", Driver: "frankfurter-v2"},
		{Code: "c", Driver: "frankfurter-v2"},
		{Code: "d", Driver: "exchangerate-api-open"},
	}
	ordered := prioritizeProviderDrivers(configs)
	if len(ordered) != len(configs) || ordered[0].Code != "a" || ordered[1].Code != "d" {
		t.Fatalf("independent driver was not promoted: %#v", ordered)
	}
}
