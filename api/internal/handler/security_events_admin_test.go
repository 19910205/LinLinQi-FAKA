package handler

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSecurityDispositionValidationRejectsUnsafeEvidence(t *testing.T) {
	if value, ok := safeDispositionText("  已确认是失窃设备登录  ", 4, 500, false); !ok || value != "已确认是失窃设备登录" {
		t.Fatalf("valid conclusion rejected: %q %v", value, ok)
	}
	for _, value := range []string{"短", "包含\x00空字节", strings.Repeat("x", 501)} {
		if _, ok := safeDispositionText(value, 4, 500, false); ok {
			t.Fatalf("unsafe conclusion accepted: %q", value)
		}
	}
	if value, ok := safeDispositionText("工单：SEC-2026-001\n证据：已撤销全部会话", 4, 2000, true); !ok || !strings.Contains(value, "\n") {
		t.Fatalf("multiline evidence rejected: %q %v", value, ok)
	}
}

func TestSecurityResolutionHistoryPreservesOriginalDetails(t *testing.T) {
	details := map[string]any{"request_id": "req-1", "score": float64(90)}
	encoded, err := appendSecurityResolution(details, map[string]any{
		"action": "resolved", "conclusion": "confirmed", "evidence": "ticket-001",
	})
	if err != nil {
		t.Fatalf("append history: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(encoded), &decoded); err != nil {
		t.Fatalf("decode history: %v", err)
	}
	if decoded["request_id"] != "req-1" || decoded["score"] != float64(90) {
		t.Fatalf("original evidence was lost: %#v", decoded)
	}
	history, ok := decoded["resolution_history"].([]any)
	if !ok || len(history) != 1 {
		t.Fatalf("resolution history missing: %#v", decoded)
	}
	if _, err := appendSecurityResolution(map[string]any{"oversized": strings.Repeat("x", 33<<10)}, map[string]any{"action": "resolved"}); err == nil {
		t.Fatal("oversized security history was accepted")
	}
}

func TestDecodedSecurityEventDetailsNeverReturnsInvalidRawJSON(t *testing.T) {
	if details := decodedSecurityEventDetails(`{"safe":true}`); details["safe"] != true {
		t.Fatalf("valid details lost: %#v", details)
	}
	if details := decodedSecurityEventDetails(`not-json`); len(details) != 0 {
		t.Fatalf("invalid details exposed: %#v", details)
	}
}
