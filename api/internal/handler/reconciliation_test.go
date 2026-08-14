package handler

import (
	"strings"
	"testing"
	"time"
)

func TestParseReconciliationStatement(t *testing.T) {
	from := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)
	csvBody := strings.Join([]string{
		"provider_trade_no,amount_minor,currency,status,occurred_at,direction",
		"PAY-1,1200,CNY,succeeded,2026-08-01T01:00:00Z,payment",
		"REF-1,300,CNY,refunded,2026-08-01T02:00:00Z,refund",
		"FAILED-1,99,CNY,failed,2026-08-01T03:00:00Z,payment",
	}, "\n")
	rows, err := parseReconciliationStatement([]byte(csvBody), from, to)
	if err != nil {
		t.Fatalf("parse valid statement: %v", err)
	}
	if len(rows) != 2 || rows[0].Direction != "payment" || rows[1].Direction != "refund" || rows[1].Amount != 300 {
		t.Fatalf("unexpected parsed rows: %#v", rows)
	}
}

func TestParseReconciliationStatementRejectsUnsafeRows(t *testing.T) {
	from := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)
	for name, body := range map[string]string{
		"missing columns":  "trade,amount\nA,1",
		"duplicate":        "provider_trade_no,amount_minor,occurred_at\nA,1,2026-08-01T01:00:00Z\nA,1,2026-08-01T02:00:00Z",
		"decimal amount":   "provider_trade_no,amount_minor,occurred_at\nA,1.25,2026-08-01T01:00:00Z",
		"outside period":   "provider_trade_no,amount_minor,occurred_at\nA,1,2026-08-02T01:00:00Z",
		"foreign currency": "provider_trade_no,amount_minor,currency,occurred_at\nA,1,USD,2026-08-01T01:00:00Z",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseReconciliationStatement([]byte(body), from, to); err == nil {
				t.Fatal("invalid statement was accepted")
			}
		})
	}
}
