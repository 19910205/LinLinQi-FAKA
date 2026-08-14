package handler

import (
	"math"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func analyticsTestContext(target string) *gin.Context {
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest("GET", target, nil)
	return context
}

func TestParseAnalyticsRangeDefaultsToThirtyDays(t *testing.T) {
	now := time.Date(2026, time.August, 9, 8, 30, 0, 0, time.UTC)
	window, err := parseAnalyticsRange(analyticsTestContext("/analytics/overview"), now)
	if err != nil {
		t.Fatalf("parse range: %v", err)
	}
	if window.Granularity != "day" || !window.To.Equal(now) || window.To.Sub(window.From) != analyticsDefaultRange {
		t.Fatalf("unexpected default window: %#v", window)
	}
}

func TestParseAnalyticsRangeRejectsLongHourlyWindow(t *testing.T) {
	context := analyticsTestContext("/analytics/overview?from=2026-08-01T00%3A00%3A00Z&to=2026-08-09T00%3A00%3A00Z&granularity=hour")
	if _, err := parseAnalyticsRange(context, time.Now()); err == nil {
		t.Fatal("expected an hourly range longer than seven days to be rejected")
	}
}

func TestBuildAnalyticsSeriesFillsMissingBuckets(t *testing.T) {
	window := analyticsRange{
		From:        time.Date(2026, time.August, 1, 12, 30, 0, 0, time.UTC),
		To:          time.Date(2026, time.August, 3, 1, 0, 0, 0, time.UTC),
		Granularity: "day",
	}
	points, indices := buildAnalyticsSeries(window)
	if len(points) != 3 || len(indices) != 3 {
		t.Fatalf("expected three daily buckets, got points=%d indices=%d", len(points), len(indices))
	}
	if !points[0].Bucket.Equal(time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected first bucket: %s", points[0].Bucket)
	}
}

func TestParseAnalyticsAmountAndCheckedAddition(t *testing.T) {
	value, err := parseAnalyticsAmount("9223372036854775807")
	if err != nil || value != math.MaxInt64 {
		t.Fatalf("parse max int64: value=%d err=%v", value, err)
	}
	if _, err := parseAnalyticsAmount("9223372036854775808"); err == nil {
		t.Fatal("expected overflow amount to be rejected")
	}
	if _, err := addAnalyticsAmount(math.MaxInt64, 1); err == nil {
		t.Fatal("expected checked addition overflow to be rejected")
	}
}
