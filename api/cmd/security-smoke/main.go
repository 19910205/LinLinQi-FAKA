// Command security-smoke performs bounded security and concurrency checks against
// a LinLinQi API running on the local machine. It intentionally refuses every
// non-loopback target and keeps state-changing financial checks opt-in.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	maxConcurrency   = 16
	maxRequestBudget = 500
	maxResponseBytes = 1 << 20
)

type config struct {
	BaseURL           string
	Concurrency       int
	RequestBudget     int
	QuoteRequests     int
	RateLimitRequests int
	Timeout           time.Duration
	OutputDir         string
	AllowRegister     bool
	AllowFinancial    bool
	OrderReplays      int
	InventoryRace     bool
	Currency          string
	ProductID         string
	VariantID         string
	PaymentChannel    string
	Email             string
	Password          string
}

type result struct {
	Name       string `json:"name"`
	Category   string `json:"category"`
	Status     string `json:"status"`
	Severity   string `json:"severity,omitempty"`
	HTTPStatus int    `json:"http_status,omitempty"`
	Requests   int    `json:"requests"`
	DurationMS int64  `json:"duration_ms"`
	Note       string `json:"note,omitempty"`
}

type report struct {
	Tool       string         `json:"tool"`
	Version    string         `json:"version"`
	Target     string         `json:"target"`
	StartedAt  time.Time      `json:"started_at"`
	FinishedAt time.Time      `json:"finished_at"`
	Limits     map[string]int `json:"limits"`
	Summary    map[string]int `json:"summary"`
	Results    []result       `json:"results"`
}

type envelope struct {
	Code int             `json:"code"`
	Data json.RawMessage `json:"data"`
}

type response struct {
	Status int
	Body   []byte
}

type runner struct {
	cfg       config
	base      *url.URL
	client    *http.Client
	semaphore chan struct{}
	used      atomic.Int64
	mu        sync.Mutex
	results   []result
	token     string
	refresh   string
	productID string
	variantID string
	stock     int
	channel   string
}

func main() {
	cfg := parseFlags()
	if err := validateConfig(&cfg); err != nil {
		fmt.Fprintln(os.Stderr, "configuration rejected:", err)
		os.Exit(2)
	}
	r, err := newRunner(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "startup rejected:", err)
		os.Exit(2)
	}
	started := time.Now().UTC()
	r.run()
	finished := time.Now().UTC()
	rep := r.report(started, finished)
	jsonPath, markdownPath, err := writeReport(cfg.OutputDir, rep)
	if err != nil {
		fmt.Fprintln(os.Stderr, "write report:", err)
		os.Exit(2)
	}
	fmt.Printf("security smoke completed: json=%s markdown=%s pass=%d fail=%d warn=%d skip=%d\n",
		jsonPath, markdownPath, rep.Summary["pass"], rep.Summary["fail"], rep.Summary["warn"], rep.Summary["skip"])
	if rep.Summary["fail"] > 0 {
		os.Exit(1)
	}
}

func parseFlags() config {
	var cfg config
	flag.StringVar(&cfg.BaseURL, "base-url", "http://127.0.0.1:8080", "LinLinQi loopback API URL")
	flag.IntVar(&cfg.Concurrency, "concurrency", 4, "worker concurrency (1-16)")
	flag.IntVar(&cfg.RequestBudget, "max-requests", 100, "hard global request budget (1-500)")
	flag.IntVar(&cfg.QuoteRequests, "quote-requests", 20, "bounded product quote requests (0 disables)")
	flag.IntVar(&cfg.RateLimitRequests, "rate-limit-requests", 0, "invalid login requests, opt-in (13-24 recommended)")
	flag.DurationVar(&cfg.Timeout, "timeout", 8*time.Second, "per-request timeout (1s-30s)")
	flag.StringVar(&cfg.OutputDir, "output-dir", "./var/security-smoke", "report directory")
	flag.BoolVar(&cfg.AllowRegister, "allow-register", false, "create one temporary test user when credentials are absent")
	flag.BoolVar(&cfg.AllowFinancial, "allow-financial", false, "allow bounded recharge/order creation tests")
	flag.IntVar(&cfg.OrderReplays, "order-replays", 0, "duplicate order replay requests (0-3; requires --allow-financial)")
	flag.BoolVar(&cfg.InventoryRace, "inventory-race", false, "run two concurrent full-stock orders when stock is 1-20")
	flag.StringVar(&cfg.Currency, "currency", "CNY", "test currency")
	flag.StringVar(&cfg.ProductID, "product-id", "", "optional product UUID override")
	flag.StringVar(&cfg.VariantID, "variant-id", "", "optional variant UUID override")
	flag.StringVar(&cfg.PaymentChannel, "payment-channel", "", "optional channel code override")
	flag.Parse()
	cfg.Email = strings.TrimSpace(os.Getenv("LINLINQI_SMOKE_EMAIL"))
	cfg.Password = os.Getenv("LINLINQI_SMOKE_PASSWORD")
	return cfg
}

func validateConfig(cfg *config) error {
	u, err := validateLoopbackURL(cfg.BaseURL)
	if err != nil {
		return err
	}
	cfg.BaseURL = strings.TrimRight(u.String(), "/")
	if cfg.Concurrency < 1 || cfg.Concurrency > maxConcurrency {
		return fmt.Errorf("concurrency must be between 1 and %d", maxConcurrency)
	}
	if cfg.RequestBudget < 1 || cfg.RequestBudget > maxRequestBudget {
		return fmt.Errorf("max-requests must be between 1 and %d", maxRequestBudget)
	}
	if cfg.QuoteRequests < 0 || cfg.QuoteRequests > 200 || cfg.RateLimitRequests < 0 || cfg.RateLimitRequests > 24 {
		return errors.New("quote requests must be 0-200 and rate-limit requests 0-24")
	}
	if cfg.Timeout < time.Second || cfg.Timeout > 30*time.Second {
		return errors.New("timeout must be between 1s and 30s")
	}
	if cfg.OrderReplays < 0 || cfg.OrderReplays > 3 {
		return errors.New("order-replays must be between 0 and 3")
	}
	if (cfg.OrderReplays > 0 || cfg.InventoryRace) && !cfg.AllowFinancial {
		return errors.New("order tests require --allow-financial")
	}
	cfg.Currency = strings.ToUpper(strings.TrimSpace(cfg.Currency))
	if len(cfg.Currency) != 3 {
		return errors.New("currency must be a three-letter code")
	}
	if (cfg.Email == "") != (cfg.Password == "") {
		return errors.New("LINLINQI_SMOKE_EMAIL and LINLINQI_SMOKE_PASSWORD must be set together")
	}
	return nil
}

func validateLoopbackURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("base URL must be an absolute URL without credentials, query, or fragment")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, errors.New("base URL scheme must be http or https")
	}
	host := strings.ToLower(u.Hostname())
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		return nil, errors.New("target host must be exactly 127.0.0.1, localhost, or ::1")
	}
	if u.Path != "" && u.Path != "/" {
		return nil, errors.New("base URL must not contain a path")
	}
	u.Path = ""
	return u, nil
}

func newRunner(cfg config) (*runner, error) {
	base, err := validateLoopbackURL(cfg.BaseURL)
	if err != nil {
		return nil, err
	}
	dialer := &net.Dialer{Timeout: cfg.Timeout, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, errors.New("invalid dial address")
			}
			if h := strings.ToLower(strings.Trim(host, "[]")); h != "127.0.0.1" && h != "localhost" && h != "::1" {
				return nil, errors.New("non-loopback dial rejected")
			}
			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil || len(ips) == 0 {
				return nil, errors.New("loopback target resolution failed")
			}
			for _, ip := range ips {
				if !ip.IP.IsLoopback() {
					return nil, errors.New("resolved non-loopback address rejected")
				}
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
		},
		MaxIdleConns: cfg.Concurrency * 2, MaxIdleConnsPerHost: cfg.Concurrency,
		IdleConnTimeout: 30 * time.Second, TLSHandshakeTimeout: cfg.Timeout,
	}
	client := &http.Client{Transport: transport, Timeout: cfg.Timeout}
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return errors.New("redirect limit exceeded")
		}
		_, err := validateLoopbackURL(req.URL.Scheme + "://" + req.URL.Host)
		return err
	}
	return &runner{cfg: cfg, base: base, client: client, semaphore: make(chan struct{}, cfg.Concurrency), productID: cfg.ProductID, variantID: cfg.VariantID, channel: cfg.PaymentChannel}, nil
}

func (r *runner) run() {
	r.health()
	r.discoverCatalog()
	r.injectionChecks()
	r.quoteLoad()
	r.forgedCallback()
	r.authenticate()
	r.authzChecks()
	r.webhookSSRF()
	r.financialChecks()
	r.refreshReplay()
	r.loginRateLimit()
}

func (r *runner) request(ctx context.Context, method, path string, body any, headers map[string]string) (response, error) {
	if r.used.Add(1) > int64(r.cfg.RequestBudget) {
		return response{}, errors.New("global request budget exhausted")
	}
	u, err := r.base.Parse(path)
	if err != nil || u.Scheme != r.base.Scheme || u.Host != r.base.Host {
		return response{}, errors.New("request escaped configured origin")
	}
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return response{}, err
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), reader)
	if err != nil {
		return response{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Language", "en-US")
	req.Header.Set("User-Agent", "LinLinQi-Security-Smoke/1.0")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	r.semaphore <- struct{}{}
	defer func() { <-r.semaphore }()
	resp, err := r.client.Do(req)
	if err != nil {
		return response{}, err
	}
	defer resp.Body.Close()
	payload, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if readErr != nil {
		return response{}, readErr
	}
	if len(payload) > maxResponseBytes {
		return response{}, errors.New("response exceeded 1 MiB safety limit")
	}
	return response{Status: resp.StatusCode, Body: payload}, nil
}

func (r *runner) add(item result) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.results = append(r.results, item)
}

func (r *runner) skip(name, category, note string) {
	r.add(result{Name: name, Category: category, Status: "skip", Requests: 0, Note: note})
}

func timed(start time.Time) int64 { return time.Since(start).Milliseconds() }

func statusResult(name, category string, start time.Time, resp response, err error, allowed ...int) result {
	item := result{Name: name, Category: category, Requests: 1, DurationMS: timed(start), HTTPStatus: resp.Status}
	if err != nil {
		item.Status, item.Severity, item.Note = "fail", "high", safeError(err)
		return item
	}
	for _, expected := range allowed {
		if resp.Status == expected {
			item.Status = "pass"
			return item
		}
	}
	item.Status, item.Severity, item.Note = "fail", "high", fmt.Sprintf("unexpected HTTP status %d", resp.Status)
	return item
}

func safeError(err error) string {
	if err == nil {
		return ""
	}
	value := err.Error()
	if len(value) > 180 {
		value = value[:180]
	}
	return value
}

func decodeEnvelope(body []byte, target any) error {
	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return err
	}
	if env.Code != 0 {
		return fmt.Errorf("API code %d", env.Code)
	}
	if target == nil {
		return nil
	}
	return json.Unmarshal(env.Data, target)
}

func (r *runner) health() {
	start := time.Now()
	resp, err := r.request(context.Background(), http.MethodGet, "/health", nil, nil)
	r.add(statusResult("health", "availability", start, resp, err, 200))
}

func (r *runner) discoverCatalog() {
	start := time.Now()
	resp, err := r.request(context.Background(), http.MethodGet, "/api/v1/products?currency="+url.QueryEscape(r.cfg.Currency)+"&page_size=20", nil, nil)
	item := statusResult("catalog_discovery", "catalog", start, resp, err, 200)
	if item.Status != "pass" {
		r.add(item)
		return
	}
	var data struct {
		Items []struct {
			Product struct {
				ID   string `json:"id"`
				Slug string `json:"slug"`
			} `json:"product"`
			Stock int `json:"stock"`
		} `json:"items"`
	}
	if err := decodeEnvelope(resp.Body, &data); err != nil {
		item.Status, item.Severity, item.Note = "fail", "medium", "catalog response schema invalid"
	} else if len(data.Items) == 0 {
		item.Status, item.Note = "warn", "no on-sale product; quote and order checks will be skipped"
	} else {
		selected := data.Items[0]
		if r.productID != "" {
			for _, candidate := range data.Items {
				if candidate.Product.ID == r.productID {
					selected = candidate
					break
				}
			}
		} else {
			r.productID = selected.Product.ID
		}
		r.stock = selected.Stock
		if selected.Product.Slug != "" && selected.Product.ID == r.productID {
			r.discoverProductDetail(selected.Product.Slug)
		} else if selected.Product.ID != r.productID {
			r.stock = 0
		}
	}
	r.add(item)

	start = time.Now()
	resp, err = r.request(context.Background(), http.MethodGet, "/api/v1/payment-channels?currency="+url.QueryEscape(r.cfg.Currency), nil, nil)
	channels := []struct {
		Code string `json:"code"`
	}{}
	channelResult := statusResult("payment_channel_discovery", "payment", start, resp, err, 200)
	if channelResult.Status == "pass" && decodeEnvelope(resp.Body, &channels) == nil && r.channel == "" && len(channels) > 0 {
		r.channel = channels[0].Code
	}
	if channelResult.Status == "pass" && r.channel == "" {
		channelResult.Status, channelResult.Note = "warn", "no enabled channel for selected currency"
	}
	r.add(channelResult)
}

func (r *runner) discoverProductDetail(slug string) {
	start := time.Now()
	resp, err := r.request(context.Background(), http.MethodGet, "/api/v1/products/"+url.PathEscape(slug)+"?currency="+url.QueryEscape(r.cfg.Currency), nil, nil)
	item := statusResult("product_detail_discovery", "catalog", start, resp, err, 200)
	if item.Status == "pass" {
		var data struct {
			Stock    int `json:"stock"`
			Variants []struct {
				ID    string `json:"id"`
				Stock int    `json:"stock"`
			} `json:"variants"`
		}
		if decodeEnvelope(resp.Body, &data) != nil {
			item.Status, item.Severity, item.Note = "fail", "medium", "product detail response schema invalid"
		} else if r.variantID == "" && len(data.Variants) > 0 {
			for _, variant := range data.Variants {
				if variant.Stock > 0 {
					r.variantID, r.stock = variant.ID, variant.Stock
					break
				}
			}
		} else if len(data.Variants) == 0 {
			r.stock = data.Stock
		}
	}
	r.add(item)
}

func (r *runner) injectionChecks() {
	payload := "' OR 1=1 -- <script>alert(1)</script>"
	start := time.Now()
	resp, err := r.request(context.Background(), http.MethodGet, "/api/v1/products?q="+url.QueryEscape(payload)+"&page_size=5", nil, nil)
	item := statusResult("catalog_sql_xss_payload", "injection", start, resp, err, 200)
	if item.Status == "pass" && bytes.Contains(resp.Body, []byte(payload)) {
		item.Status, item.Severity, item.Note = "fail", "medium", "unescaped attack marker was reflected in JSON"
	}
	r.add(item)

	start = time.Now()
	resp, err = r.request(context.Background(), http.MethodPost, "/api/v1/checkout/quote", map[string]any{
		"lines": []map[string]any{{"product_id": "' OR 1=1 --", "quantity": 1}}, "payment_method": "sandbox", "currency": r.cfg.Currency,
	}, nil)
	r.add(statusResult("quote_identifier_injection", "injection", start, resp, err, 400, 422))
}

func (r *runner) quoteLoad() {
	if r.cfg.QuoteRequests == 0 {
		r.skip("quote_concurrency", "load", "disabled by quote-requests=0")
		return
	}
	if r.productID == "" || r.channel == "" {
		r.skip("quote_concurrency", "load", "product or payment channel unavailable")
		return
	}
	body := map[string]any{"lines": []map[string]any{{"product_id": r.productID, "variant_id": r.variantID, "quantity": 1}}, "contact": "a2456836", "payment_method": r.channel, "currency": r.cfg.Currency}
	start := time.Now()
	statuses := make(chan int, r.cfg.QuoteRequests)
	errs := make(chan error, r.cfg.QuoteRequests)
	var wg sync.WaitGroup
	for i := 0; i < r.cfg.QuoteRequests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := r.request(context.Background(), http.MethodPost, "/api/v1/checkout/quote", body, nil)
			if err != nil {
				errs <- err
				return
			}
			statuses <- resp.Status
		}()
	}
	wg.Wait()
	close(statuses)
	close(errs)
	failures, throttled := len(errs), 0
	for code := range statuses {
		if code == 429 {
			throttled++
		} else if code != 200 {
			failures++
		}
	}
	item := result{Name: "quote_concurrency", Category: "load", Status: "pass", Requests: r.cfg.QuoteRequests, DurationMS: timed(start), Note: fmt.Sprintf("bounded concurrency=%d throttled=%d", r.cfg.Concurrency, throttled)}
	if failures > 0 {
		item.Status, item.Severity, item.Note = "fail", "medium", fmt.Sprintf("%d quote requests failed; throttled=%d", failures, throttled)
	}
	r.add(item)
}

func (r *runner) forgedCallback() {
	if r.channel == "" {
		r.skip("forged_payment_callback", "payment", "no channel discovered")
		return
	}
	start := time.Now()
	resp, err := r.request(context.Background(), http.MethodPost, "/api/v1/payments/"+url.PathEscape(r.channel)+"/callback", map[string]any{
		"event_id": "forged-smoke", "intent_no": "nonexistent", "amount": 1, "status": "succeeded",
	}, map[string]string{"X-Timestamp": "0", "X-Signature": "invalid"})
	item := statusResult("forged_payment_callback", "payment", start, resp, err, 400, 401, 404, 503)
	if item.Status == "pass" && resp.Status != 401 {
		item.Status, item.Note = "warn", fmt.Sprintf("rejected with HTTP %d before signature verification", resp.Status)
	}
	r.add(item)
}

func (r *runner) authenticate() {
	email, password := r.cfg.Email, r.cfg.Password
	if email == "" && r.cfg.AllowRegister {
		random := randomHex(10)
		email, password = "linlinqi-smoke-"+random+"@example.invalid", "Lq!"+random+"aZ9"
		start := time.Now()
		resp, err := r.request(context.Background(), http.MethodPost, "/api/v1/auth/register", map[string]any{"email": email, "password": password, "nickname": "LinLinQi Security Smoke"}, nil)
		item := statusResult("temporary_user_registration", "auth", start, resp, err, 201)
		if item.Status == "pass" {
			var auth struct {
				Token   string `json:"token"`
				Refresh string `json:"refresh_token"`
			}
			if decodeEnvelope(resp.Body, &auth) != nil || auth.Token == "" || auth.Refresh == "" {
				item.Status, item.Severity, item.Note = "fail", "high", "registration omitted session tokens"
			} else {
				r.token, r.refresh = auth.Token, auth.Refresh
			}
		}
		r.add(item)
	}
	if email == "" {
		r.skip("correct_login", "auth", "set credential environment variables or --allow-register")
		return
	}
	if r.token == "" {
		start := time.Now()
		resp, err := r.request(context.Background(), http.MethodPost, "/api/v1/auth/login", map[string]any{"account": email, "password": password}, nil)
		item := statusResult("correct_login", "auth", start, resp, err, 200)
		if item.Status == "pass" {
			var auth struct {
				Token   string `json:"token"`
				Refresh string `json:"refresh_token"`
			}
			if decodeEnvelope(resp.Body, &auth) != nil || auth.Token == "" || auth.Refresh == "" {
				item.Status, item.Severity, item.Note = "fail", "high", "login omitted session tokens"
			} else {
				r.token, r.refresh = auth.Token, auth.Refresh
			}
		}
		r.add(item)
	}
	start := time.Now()
	resp, err := r.request(context.Background(), http.MethodPost, "/api/v1/auth/login", map[string]any{"account": email, "password": password + "-wrong"}, nil)
	r.add(statusResult("wrong_password_rejected", "auth", start, resp, err, 401, 429))
}

func (r *runner) authzChecks() {
	if r.token == "" {
		r.skip("jwt_tamper", "authz", "authenticated session unavailable")
		r.skip("idor_ticket", "authz", "authenticated session unavailable")
		return
	}
	bad := tamperToken(r.token)
	start := time.Now()
	resp, err := r.request(context.Background(), http.MethodGet, "/api/v1/me", nil, map[string]string{"Authorization": "Bearer " + bad})
	r.add(statusResult("jwt_tamper", "authz", start, resp, err, 401))
	start = time.Now()
	resp, err = r.request(context.Background(), http.MethodGet, "/api/v1/me/tickets/00000000-0000-4000-8000-000000000001", nil, r.authHeaders())
	r.add(statusResult("idor_ticket", "authz", start, resp, err, 404))
	start = time.Now()
	resp, err = r.request(context.Background(), http.MethodGet, "/api/v1/orders/LQ-NOT-FOUND", nil, map[string]string{"X-Order-Token": strings.Repeat("A", 48)})
	r.add(statusResult("order_lookup_token_enforced", "authz", start, resp, err, 404))
}

func (r *runner) webhookSSRF() {
	if r.token == "" {
		r.skip("webhook_ssrf", "ssrf", "authenticated session unavailable")
		return
	}
	targets := []string{"http://127.0.0.1:8080/health", "http://localhost:8080/health", "http://169.254.169.254/latest/meta-data"}
	start := time.Now()
	failures := 0
	for _, target := range targets {
		resp, err := r.request(context.Background(), http.MethodPost, "/api/v1/me/webhooks", map[string]any{"url": target, "events": []string{"order.delivered"}}, r.authHeaders())
		// A per-user webhook rate limit is also a secure rejection. Once the
		// first SSRF payload is rejected, subsequent probes may be throttled
		// before URL validation; neither path performs an outbound request.
		if err != nil || (resp.Status != 422 && resp.Status != 429) {
			failures++
		}
	}
	item := result{Name: "webhook_ssrf", Category: "ssrf", Status: "pass", Requests: len(targets), DurationMS: timed(start)}
	if failures > 0 {
		item.Status, item.Severity, item.Note = "fail", "critical", fmt.Sprintf("%d forbidden outbound URL payloads were not rejected with HTTP 422", failures)
	}
	r.add(item)
}

func (r *runner) financialChecks() {
	if !r.cfg.AllowFinancial {
		r.skip("recharge_idempotency", "financial", "requires explicit --allow-financial")
		r.skip("order_replay", "financial", "requires explicit --allow-financial")
		r.skip("inventory_race", "financial", "requires explicit --allow-financial")
		return
	}
	if r.token == "" || r.channel == "" {
		r.skip("recharge_idempotency", "financial", "session or channel unavailable")
	} else {
		r.rechargeIdempotency()
	}
	if r.cfg.OrderReplays > 0 {
		r.orderReplay()
	} else {
		r.skip("order_replay", "financial", "disabled by order-replays=0")
	}
	if r.cfg.InventoryRace {
		r.inventoryConcurrency()
	} else {
		r.skip("inventory_race", "financial", "disabled")
	}
}

func (r *runner) rechargeIdempotency() {
	key := "smoke-recharge-" + randomHex(16)
	body := map[string]any{"amount": 100, "channel_code": r.channel, "currency": r.cfg.Currency}
	start := time.Now()
	responses := make(chan response, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := r.request(context.Background(), http.MethodPost, "/api/v1/me/recharges", body, mergeHeaders(r.authHeaders(), map[string]string{"Idempotency-Key": key}))
			if err != nil {
				errs <- err
			} else {
				responses <- resp
			}
		}()
	}
	wg.Wait()
	close(responses)
	close(errs)
	ids := map[string]struct{}{}
	bad := len(errs)
	for resp := range responses {
		if resp.Status != 200 && resp.Status != 201 && resp.Status != 409 && resp.Status != 502 {
			bad++
			continue
		}
		var data struct {
			Recharge struct {
				ID string `json:"id"`
			} `json:"recharge"`
		}
		if (resp.Status == 200 || resp.Status == 201) && decodeEnvelope(resp.Body, &data) == nil && data.Recharge.ID != "" {
			ids[data.Recharge.ID] = struct{}{}
		}
	}
	item := result{Name: "recharge_idempotency", Category: "financial", Status: "pass", Requests: 2, DurationMS: timed(start), Note: fmt.Sprintf("unique recharge records observed=%d", len(ids))}
	if bad > 0 || len(ids) > 1 {
		item.Status, item.Severity, item.Note = "fail", "critical", "concurrent reuse produced errors or multiple recharge records"
	} else if len(ids) == 0 {
		item.Status, item.Note = "warn", "provider returned no durable recharge DTO; uniqueness was not fully observable"
	}
	r.add(item)
	start = time.Now()
	body["amount"] = 200
	resp, err := r.request(context.Background(), http.MethodPost, "/api/v1/me/recharges", body, mergeHeaders(r.authHeaders(), map[string]string{"Idempotency-Key": key}))
	r.add(statusResult("recharge_idempotency_conflict", "financial", start, resp, err, 409))
}

func (r *runner) orderReplay() {
	if r.productID == "" || r.channel == "" {
		r.skip("order_replay", "financial", "product or channel unavailable")
		return
	}
	key := "smoke-order-" + randomHex(16)
	body := r.orderBody(1, key)
	start := time.Now()
	unique := map[string]struct{}{}
	created := 0
	for i := 0; i < r.cfg.OrderReplays; i++ {
		resp, err := r.request(context.Background(), http.MethodPost, "/api/v1/orders", body, map[string]string{"Idempotency-Key": key})
		if err != nil {
			continue
		}
		if resp.Status == 201 {
			created++
			var data struct {
				ID string `json:"id"`
			}
			if decodeEnvelope(resp.Body, &data) == nil {
				unique[data.ID] = struct{}{}
			}
		}
	}
	item := result{Name: "order_replay", Category: "financial", Status: "pass", Requests: r.cfg.OrderReplays, DurationMS: timed(start), Note: fmt.Sprintf("created=%d unique_orders=%d", created, len(unique))}
	if created > 1 && len(unique) > 1 {
		item.Status, item.Severity, item.Note = "fail", "high", "duplicate request created multiple orders; storefront order idempotency is missing"
	} else if created == 0 {
		item.Status, item.Note = "warn", "no order was created; replay behavior was not exercised"
	}
	r.add(item)
}

func (r *runner) inventoryConcurrency() {
	if r.productID == "" || r.channel == "" || r.stock < 1 || r.stock > 20 {
		r.skip("inventory_race", "financial", "requires discovered local stock between 1 and 20")
		return
	}
	start := time.Now()
	successes := atomic.Int64{}
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			key := "smoke-stock-" + randomHex(16)
			resp, err := r.request(context.Background(), http.MethodPost, "/api/v1/orders", r.orderBody(r.stock, key), map[string]string{"Idempotency-Key": key})
			if err == nil && resp.Status == 201 {
				successes.Add(1)
			}
		}(i)
	}
	wg.Wait()
	item := result{Name: "inventory_race", Category: "financial", Status: "pass", Requests: 2, DurationMS: timed(start), Note: fmt.Sprintf("initial_stock=%d successful_full_stock_orders=%d", r.stock, successes.Load())}
	if successes.Load() > 1 {
		item.Status, item.Severity, item.Note = "fail", "critical", "concurrent full-stock orders both succeeded; possible oversell"
	} else if successes.Load() == 0 {
		item.Status, item.Note = "warn", "neither order succeeded; oversell was not observed but the race was inconclusive"
	}
	r.add(item)
}

func (r *runner) orderBody(quantity int, clientOrderNo string) map[string]any {
	return map[string]any{"product_id": r.productID, "variant_id": r.variantID, "quantity": quantity, "email": "linlinqi-order-smoke@example.invalid", "payment_method": r.channel, "client_order_no": clientOrderNo, "currency": r.cfg.Currency}
}

func (r *runner) refreshReplay() {
	if r.refresh == "" {
		r.skip("refresh_token_replay", "auth", "refresh token unavailable")
		return
	}
	old := r.refresh
	start := time.Now()
	first, err := r.request(context.Background(), http.MethodPost, "/api/v1/auth/refresh", map[string]any{"refresh_token": old}, nil)
	if err != nil || first.Status != 200 {
		r.add(statusResult("refresh_token_rotation", "auth", start, first, err, 200))
		return
	}
	var data struct {
		Refresh string `json:"refresh_token"`
	}
	if decodeEnvelope(first.Body, &data) != nil || data.Refresh == "" {
		r.add(result{Name: "refresh_token_rotation", Category: "auth", Status: "fail", Severity: "high", Requests: 1, DurationMS: timed(start), Note: "refresh response omitted replacement token"})
		return
	}
	r.add(result{Name: "refresh_token_rotation", Category: "auth", Status: "pass", Requests: 1, DurationMS: timed(start)})
	start = time.Now()
	replayed, replayErr := r.request(context.Background(), http.MethodPost, "/api/v1/auth/refresh", map[string]any{"refresh_token": old}, nil)
	r.add(statusResult("refresh_token_replay", "auth", start, replayed, replayErr, 401))
	// Never expose or persist either refresh token in the report.
	r.refresh = ""
}

func (r *runner) loginRateLimit() {
	if r.cfg.RateLimitRequests == 0 {
		r.skip("login_rate_limit", "rate_limit", "disabled; set rate-limit-requests explicitly")
		return
	}
	start := time.Now()
	throttled, unexpected := 0, 0
	for i := 0; i < r.cfg.RateLimitRequests; i++ {
		resp, err := r.request(context.Background(), http.MethodPost, "/api/v1/auth/login", map[string]any{"account": "rate-limit-" + randomHex(4) + "@example.invalid", "password": "DefinitelyWrong!9"}, nil)
		if err != nil {
			unexpected++
		} else if resp.Status == 429 {
			throttled++
		} else if resp.Status != 401 {
			unexpected++
		}
	}
	item := result{Name: "login_rate_limit", Category: "rate_limit", Status: "pass", Requests: r.cfg.RateLimitRequests, DurationMS: timed(start), Note: fmt.Sprintf("throttled=%d", throttled)}
	if unexpected > 0 {
		item.Status, item.Severity, item.Note = "fail", "medium", fmt.Sprintf("unexpected responses=%d throttled=%d", unexpected, throttled)
	} else if r.cfg.RateLimitRequests >= 13 && throttled == 0 {
		item.Status, item.Severity, item.Note = "fail", "high", "no HTTP 429 observed after configured login limit"
	} else if throttled == 0 {
		item.Status, item.Note = "warn", "request count was below or did not reach the current limiter window"
	}
	r.add(item)
}

func (r *runner) authHeaders() map[string]string {
	return map[string]string{"Authorization": "Bearer " + r.token}
}

func mergeHeaders(left, right map[string]string) map[string]string {
	result := map[string]string{}
	for k, v := range left {
		result[k] = v
	}
	for k, v := range right {
		result[k] = v
	}
	return result
}

func randomHex(bytesCount int) string {
	b := make([]byte, bytesCount)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func tamperToken(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[2] == "" {
		return token + "x"
	}
	first := parts[2][0]
	replacement := byte('A')
	if first == replacement {
		replacement = 'B'
	}
	parts[2] = string(replacement) + parts[2][1:]
	return strings.Join(parts, ".")
}

func (r *runner) report(started, finished time.Time) report {
	r.mu.Lock()
	items := append([]result(nil), r.results...)
	r.mu.Unlock()
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Category == items[j].Category {
			return items[i].Name < items[j].Name
		}
		return items[i].Category < items[j].Category
	})
	summary := map[string]int{"pass": 0, "fail": 0, "warn": 0, "skip": 0}
	for _, item := range items {
		summary[item.Status]++
	}
	return report{Tool: "LinLinQi security-smoke", Version: "1.0.0", Target: r.cfg.BaseURL, StartedAt: started, FinishedAt: finished, Limits: map[string]int{"concurrency": r.cfg.Concurrency, "request_budget": r.cfg.RequestBudget, "requests_used": int(r.used.Load())}, Summary: summary, Results: items}
}

func writeReport(dir string, rep report) (string, string, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", "", err
	}
	stamp := rep.StartedAt.Format("20060102T150405Z")
	jsonPath := filepath.Join(dir, "linlinqi-security-smoke-"+stamp+".json")
	markdownPath := filepath.Join(dir, "linlinqi-security-smoke-"+stamp+".md")
	payload, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return "", "", err
	}
	if err := os.WriteFile(jsonPath, append(payload, '\n'), 0600); err != nil {
		return "", "", err
	}
	var md strings.Builder
	fmt.Fprintf(&md, "# LinLinQi security smoke report\n\n- Target: `%s`\n- Started: `%s`\n- Requests: %d/%d\n- Concurrency: %d\n- Summary: pass=%d, fail=%d, warn=%d, skip=%d\n\n", rep.Target, rep.StartedAt.Format(time.RFC3339), rep.Limits["requests_used"], rep.Limits["request_budget"], rep.Limits["concurrency"], rep.Summary["pass"], rep.Summary["fail"], rep.Summary["warn"], rep.Summary["skip"])
	md.WriteString("| Category | Check | Status | Severity | HTTP | Requests | Duration | Note |\n|---|---|---:|---:|---:|---:|---:|---|\n")
	for _, item := range rep.Results {
		fmt.Fprintf(&md, "| %s | %s | %s | %s | %d | %d | %d ms | %s |\n", markdownCell(item.Category), markdownCell(item.Name), item.Status, item.Severity, item.HTTPStatus, item.Requests, item.DurationMS, markdownCell(item.Note))
	}
	md.WriteString("\nSecrets, access tokens, passwords, callback payload bodies, card contents, and idempotency keys are intentionally omitted.\n")
	if err := os.WriteFile(markdownPath, []byte(md.String()), 0600); err != nil {
		return "", "", err
	}
	return jsonPath, markdownPath, nil
}

func markdownCell(value string) string {
	value = strings.ReplaceAll(value, "|", "\\|")
	value = strings.ReplaceAll(value, "\n", " ")
	if len(value) > 240 {
		value = value[:240]
	}
	return value
}
