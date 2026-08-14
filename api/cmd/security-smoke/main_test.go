package main

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return fn(req) }

func validTestConfig(target string) config {
	return config{BaseURL: target, Concurrency: 2, RequestBudget: 20, QuoteRequests: 0, Timeout: 2 * time.Second, Currency: "CNY"}
}

func TestValidateLoopbackURL(t *testing.T) {
	valid := []string{"http://127.0.0.1:8080", "http://localhost:8080", "http://[::1]:8080/"}
	for _, value := range valid {
		if _, err := validateLoopbackURL(value); err != nil {
			t.Fatalf("expected %s to be valid: %v", value, err)
		}
	}
	invalid := []string{"https://example.com", "http://127.0.0.2:8080", "http://0.0.0.0:8080", "http://user:pass@localhost:8080", "file:///tmp/a", "http://localhost:8080/api"}
	for _, value := range invalid {
		if _, err := validateLoopbackURL(value); err == nil {
			t.Fatalf("expected %s to be rejected", value)
		}
	}
}

func TestValidateConfigSafetyCaps(t *testing.T) {
	cfg := validTestConfig("http://127.0.0.1:8080")
	cfg.Concurrency = maxConcurrency + 1
	if err := validateConfig(&cfg); err == nil {
		t.Fatal("expected concurrency cap rejection")
	}
	cfg = validTestConfig("http://127.0.0.1:8080")
	cfg.RequestBudget = maxRequestBudget + 1
	if err := validateConfig(&cfg); err == nil {
		t.Fatal("expected budget cap rejection")
	}
	cfg = validTestConfig("http://127.0.0.1:8080")
	cfg.OrderReplays = 2
	if err := validateConfig(&cfg); err == nil {
		t.Fatal("expected financial opt-in rejection")
	}
}

func TestRunnerRequestAndBudget(t *testing.T) {
	cfg := validTestConfig("http://127.0.0.1:8080")
	cfg.RequestBudget = 1
	if err := validateConfig(&cfg); err != nil {
		t.Fatal(err)
	}
	base, _ := url.Parse(cfg.BaseURL)
	r := &runner{cfg: cfg, base: base, semaphore: make(chan struct{}, cfg.Concurrency)}
	r.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"code":0,"data":{"ok":true}}`)), Header: make(http.Header)}, nil
	})}
	if _, err := r.request(t.Context(), http.MethodGet, "/health", nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := r.request(t.Context(), http.MethodGet, "/health", nil, nil); err == nil {
		t.Fatal("expected budget exhaustion")
	}
}

func TestTamperTokenChangesSignatureOnly(t *testing.T) {
	original := "header.payload.signature"
	changed := tamperToken(original)
	if changed == original {
		t.Fatal("token was not changed")
	}
	if changed[:len("header.payload.")] != "header.payload." {
		t.Fatalf("header or payload changed: %s", changed)
	}
}

func TestMarkdownCellRemovesStructuralContent(t *testing.T) {
	if got := markdownCell("a|b\nc"); got != "a\\|b c" {
		t.Fatalf("unexpected markdown cell: %q", got)
	}
}
