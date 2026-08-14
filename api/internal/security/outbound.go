package security

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

var forbiddenOutboundPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("fec0::/10"),
}

// ValidateOutboundURL rejects endpoints that could reach local infrastructure
// in production. The explicit allowPrivate flag is reserved for non-production
// development and test environments and also permits plain HTTP there. Callers
// validate again when opening the connection to limit DNS-rebinding and
// time-of-check/time-of-use attacks.
func ValidateOutboundURL(ctx context.Context, raw string, allowPrivate bool) (*url.URL, error) {
	if len(raw) == 0 || len(raw) > 500 || strings.TrimSpace(raw) != raw {
		return nil, fmt.Errorf("invalid webhook URL")
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" || parsed.Opaque != "" {
		return nil, fmt.Errorf("invalid webhook URL")
	}
	if parsed.Scheme != "https" && !(allowPrivate && parsed.Scheme == "http") {
		return nil, fmt.Errorf("webhook URL must use HTTPS")
	}
	if parsed.User != nil || parsed.Fragment != "" {
		return nil, fmt.Errorf("webhook URL userinfo and fragments are forbidden")
	}
	hostname := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if !allowPrivate && (hostname == "localhost" || strings.HasSuffix(hostname, ".localhost") || strings.HasSuffix(hostname, ".local")) {
		return nil, fmt.Errorf("webhook URL host is forbidden")
	}
	ips, err := resolveOutboundIPs(ctx, hostname)
	if err != nil || len(ips) == 0 {
		if err == nil {
			err = fmt.Errorf("no address returned")
		}
		return nil, fmt.Errorf("resolve webhook host: %w", err)
	}
	for _, ip := range ips {
		if !allowPrivate && ForbiddenOutboundIP(ip) {
			return nil, fmt.Errorf("webhook URL resolves to a forbidden network")
		}
	}
	return parsed, nil
}

func ForbiddenOutboundIP(ip net.IP) bool {
	if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
		return true
	}
	address, ok := netip.AddrFromSlice(ip)
	if !ok {
		return true
	}
	address = address.Unmap()
	for _, prefix := range forbiddenOutboundPrefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func resolveOutboundIPs(ctx context.Context, hostname string) ([]net.IP, error) {
	if parsed := net.ParseIP(strings.TrimSpace(hostname)); parsed != nil {
		return []net.IP{parsed}, nil
	}
	return net.DefaultResolver.LookupIP(ctx, "ip", hostname)
}

func resolveValidatedOutboundAddress(ctx context.Context, address string, allowPrivate bool) (string, []net.IP, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || strings.TrimSpace(host) == "" || strings.TrimSpace(port) == "" {
		if err == nil {
			err = fmt.Errorf("host and port are required")
		}
		return "", nil, fmt.Errorf("invalid outbound address: %w", err)
	}
	ips, err := resolveOutboundIPs(ctx, host)
	if err != nil || len(ips) == 0 {
		if err == nil {
			err = fmt.Errorf("no address returned")
		}
		return "", nil, fmt.Errorf("resolve outbound host: %w", err)
	}
	for _, ip := range ips {
		if !allowPrivate && ForbiddenOutboundIP(ip) {
			return "", nil, fmt.Errorf("outbound host resolves to a forbidden network")
		}
	}
	return port, ips, nil
}

// ValidateOutboundAddress applies the same DNS and network policy used by the
// pinned dialer to non-HTTP services such as SMTP.
func ValidateOutboundAddress(ctx context.Context, address string, allowPrivate bool) error {
	_, _, err := resolveValidatedOutboundAddress(ctx, address, allowPrivate)
	return err
}

// DialOutboundContext resolves the destination once, rejects every non-public
// result in production, and dials one of the validated addresses directly.
// Resolving all results before dialing prevents a hostname with mixed public
// and private answers from being used as an SSRF bypass.
func DialOutboundContext(ctx context.Context, network, address string, allowPrivate bool) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	port, ips, err := resolveValidatedOutboundAddress(ctx, address, allowPrivate)
	if err != nil {
		return nil, err
	}
	var lastErr error
	for _, ip := range ips {
		connection, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if dialErr == nil {
			return connection, nil
		}
		lastErr = dialErr
	}
	return nil, fmt.Errorf("connect outbound host: %w", lastErr)
}

// NewOutboundHTTPClient resolves and pins a public address at dial time and
// never follows redirects. This prevents signed credentials from being sent to
// a redirect target and closes the DNS-rebinding gap after URL validation.
func NewOutboundHTTPClient(timeout time.Duration, allowPrivate bool) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		return DialOutboundContext(ctx, network, address, allowPrivate)
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}
