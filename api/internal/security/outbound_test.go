package security

import (
	"context"
	"net"
	"testing"
)

func TestValidateOutboundURLPrivateAccessIsExplicit(t *testing.T) {
	if _, err := ValidateOutboundURL(context.Background(), "http://127.0.0.1:8081/image.png", false); err == nil {
		t.Fatal("production policy accepted private HTTP endpoint")
	}
	parsed, err := ValidateOutboundURL(context.Background(), "http://127.0.0.1:8081/image.png", true)
	if err != nil || parsed.Hostname() != "127.0.0.1" {
		t.Fatalf("explicit development policy rejected local endpoint: %v", err)
	}
}

func TestForbiddenOutboundIPIncludesReservedInfrastructureRanges(t *testing.T) {
	for _, value := range []string{
		"0.0.0.1",
		"100.64.0.1",
		"192.0.2.10",
		"198.19.255.254",
		"198.51.100.4",
		"203.0.113.9",
		"240.0.0.1",
		"2001:db8::1",
		"fec0::1",
	} {
		if !ForbiddenOutboundIP(net.ParseIP(value)) {
			t.Errorf("reserved outbound address accepted: %s", value)
		}
	}
	for _, value := range []string{"1.1.1.1", "8.8.8.8", "2606:4700:4700::1111"} {
		if ForbiddenOutboundIP(net.ParseIP(value)) {
			t.Errorf("public outbound address rejected: %s", value)
		}
	}
}

func TestDialOutboundContextRejectsLiteralPrivateAddressWithoutDialing(t *testing.T) {
	if _, err := DialOutboundContext(context.Background(), "tcp", "127.0.0.1:25", false); err == nil {
		t.Fatal("production outbound dial accepted a loopback SMTP endpoint")
	}
	if err := ValidateOutboundAddress(context.Background(), "100.64.0.1:465", false); err == nil {
		t.Fatal("production outbound validation accepted a carrier-grade NAT endpoint")
	}
}
