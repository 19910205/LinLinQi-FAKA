package supply

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"linlinqi/api/internal/security"
)

type protocolTransport struct {
	baseURL string
	client  *http.Client
}

func newProtocolTransport(baseURL string, allowPrivate bool) *protocolTransport {
	return &protocolTransport{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		client:  security.NewOutboundHTTPClient(20*time.Second, allowPrivate),
	}
}

func (t *protocolTransport) endpoint(path string, query url.Values) (string, error) {
	if t.baseURL == "" || len(t.baseURL) > 500 || !strings.HasPrefix(path, "/") {
		return "", errors.New("supplier endpoint is invalid")
	}
	parsed, err := url.Parse(t.baseURL + path)
	if err != nil || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return "", errors.New("supplier endpoint is invalid")
	}
	if query != nil {
		parsed.RawQuery = query.Encode()
	}
	return parsed.String(), nil
}

func (t *protocolTransport) do(ctx context.Context, method, path string, query url.Values, body []byte, contentType string, headers http.Header) ([]byte, int, error) {
	endpoint, err := t.endpoint(path, query)
	if err != nil {
		return nil, 0, err
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, 0, errors.New("build supplier request")
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	for key, values := range headers {
		for _, value := range values {
			request.Header.Add(key, value)
		}
	}
	response, err := t.client.Do(request)
	if err != nil {
		return nil, 0, fmt.Errorf("call supplier endpoint: %w", err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, 8<<20+1))
	if err != nil {
		return nil, response.StatusCode, errors.New("read supplier response")
	}
	if len(payload) > 8<<20 {
		return nil, response.StatusCode, errors.New("supplier response exceeds limit")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, response.StatusCode, fmt.Errorf("supplier endpoint returned HTTP %d", response.StatusCode)
	}
	return payload, response.StatusCode, nil
}

func (t *protocolTransport) json(ctx context.Context, method, path string, query url.Values, input any, headers http.Header, output any) error {
	var body []byte
	var err error
	if input != nil {
		body, err = json.Marshal(input)
		if err != nil {
			return errors.New("encode supplier request")
		}
	}
	payload, _, err := t.do(ctx, method, path, query, body, "application/json", headers)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(payload, output); err != nil {
		return errors.New("decode supplier response")
	}
	return nil
}

func (t *protocolTransport) form(ctx context.Context, path string, values url.Values, headers http.Header, output any) error {
	payload, _, err := t.do(ctx, http.MethodPost, path, nil, []byte(values.Encode()), "application/x-www-form-urlencoded", headers)
	if err != nil {
		return err
	}
	if output == nil {
		return nil
	}
	if err := json.Unmarshal(payload, output); err != nil {
		return errors.New("decode supplier response")
	}
	return nil
}
