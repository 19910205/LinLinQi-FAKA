package currency

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"linlinqi/api/internal/security"
)

type providerHTTP struct {
	baseURL      string
	client       *http.Client
	allowPrivate bool
}

func newProviderHTTP(baseURL string, timeout time.Duration, allowPrivate bool) *providerHTTP {
	return &providerHTTP{baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"), client: security.NewOutboundHTTPClient(timeout, allowPrivate), allowPrivate: allowPrivate}
}

func (client *providerHTTP) get(ctx context.Context, path string, query url.Values) ([]byte, string, error) {
	endpoint, err := url.Parse(client.baseURL + path)
	if err != nil || endpoint.Hostname() == "" || endpoint.User != nil || endpoint.Fragment != "" || (endpoint.Scheme != "https" && (!client.allowPrivate || endpoint.Scheme != "http")) {
		return nil, "", errors.New("exchange-rate endpoint is invalid")
	}
	endpoint.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, "", errors.New("build exchange-rate request")
	}
	response, err := client.client.Do(request)
	if err != nil {
		return nil, "", err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, "", errors.New("exchange-rate endpoint returned non-success status")
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, 2<<20+1))
	if err != nil || len(payload) > 2<<20 {
		return nil, "", errors.New("exchange-rate response is invalid")
	}
	digest := sha256.Sum256(payload)
	return payload, hex.EncodeToString(digest[:]), nil
}
