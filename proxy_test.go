package main

import (
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestParseForwardedMetadata(t *testing.T) {
	tests := []struct {
		name    string
		headers map[string]string
		wantErr bool
	}{
		{
			name: "x forwarded",
			headers: map[string]string{
				"X-Forwarded-For":   "203.0.113.10, 10.0.0.2",
				"X-Forwarded-Proto": "https",
				"X-Forwarded-Host":  "notes.example.test",
			},
		},
		{
			name: "repeated matching x forwarded proto and host",
			headers: map[string]string{
				"X-Forwarded-For":   "203.0.113.10",
				"X-Forwarded-Proto": "https, https",
				"X-Forwarded-Host":  "notes.example.test, notes.example.test",
			},
		},
		{
			name: "standard forwarded rejected",
			headers: map[string]string{
				"Forwarded":         "for=203.0.113.10;proto=https;host=notes.example.test",
				"X-Forwarded-For":   "203.0.113.10",
				"X-Forwarded-Proto": "https",
				"X-Forwarded-Host":  "notes.example.test",
			},
			wantErr: true,
		},
		{
			name: "missing x forwarded host",
			headers: map[string]string{
				"X-Forwarded-For":   "203.0.113.10",
				"X-Forwarded-Proto": "https",
			},
			wantErr: true,
		},
		{
			name: "conflicting x forwarded proto",
			headers: map[string]string{
				"X-Forwarded-For":   "203.0.113.10",
				"X-Forwarded-Proto": "https, http",
				"X-Forwarded-Host":  "notes.example.test",
			},
			wantErr: true,
		},
		{
			name: "conflicting x forwarded host",
			headers: map[string]string{
				"X-Forwarded-For":   "203.0.113.10",
				"X-Forwarded-Proto": "https",
				"X-Forwarded-Host":  "notes.example.test, other.example.test",
			},
			wantErr: true,
		},
		{
			name: "obfuscated x forwarded for",
			headers: map[string]string{
				"X-Forwarded-For":   "_hidden",
				"X-Forwarded-Proto": "https",
				"X-Forwarded-Host":  "notes.example.test",
			},
			wantErr: true,
		},
		{
			name: "unknown x forwarded for",
			headers: map[string]string{
				"X-Forwarded-For":   "unknown",
				"X-Forwarded-Proto": "https",
				"X-Forwarded-Host":  "notes.example.test",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			for key, value := range tt.headers {
				req.Header.Set(key, value)
			}
			meta, err := parseForwardedMetadata(req)
			if tt.wantErr {
				if err == nil {
					t.Fatal("parseForwardedMetadata returned nil error, want failure")
				}
				return
			}
			if err != nil {
				t.Fatalf("parseForwardedMetadata returned error: %v", err)
			}
			if meta.host != "notes.example.test" || meta.proto != "https" || len(meta.forChain) == 0 {
				t.Fatalf("unexpected forwarded metadata: %#v", meta)
			}
		})
	}
}

func TestParseForwardedAddr(t *testing.T) {
	tests := []struct {
		raw     string
		want    string
		wantErr bool
	}{
		{raw: "203.0.113.10", want: "203.0.113.10"},
		{raw: "203.0.113.10:443", want: "203.0.113.10"},
		{raw: "2001:db8::1", want: "2001:db8::1"},
		{raw: "[2001:db8::1]", want: "2001:db8::1"},
		{raw: "[2001:db8::1]:443", want: "2001:db8::1"},
		{raw: "unknown", wantErr: true},
		{raw: "_hidden", wantErr: true},
		{raw: "[2001:db8::1", wantErr: true},
		{raw: "[2001:db8::1]bad", wantErr: true},
		{raw: "[2001:db8::1]:bad", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			got, err := parseForwardedAddr(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatal("parseForwardedAddr returned nil error, want failure")
				}
				return
			}
			if err != nil {
				t.Fatalf("parseForwardedAddr returned error: %v", err)
			}
			if got.String() != tt.want {
				t.Fatalf("addr = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestForwardedClientIPSelectsFirstUntrustedFromRight(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")}
	chain := []netip.Addr{
		netip.MustParseAddr("203.0.113.10"),
		netip.MustParseAddr("198.51.100.5"),
		netip.MustParseAddr("10.0.0.8"),
	}

	got, err := forwardedClientIP(chain, trusted)
	if err != nil {
		t.Fatalf("forwardedClientIP returned error: %v", err)
	}
	if got.String() != "198.51.100.5" {
		t.Fatalf("client IP = %s, want 198.51.100.5", got)
	}
}

func TestForwardedClientIPRejectsAllTrustedChain(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")}
	chain := []netip.Addr{
		netip.MustParseAddr("10.0.0.7"),
		netip.MustParseAddr("10.0.0.8"),
	}

	if _, err := forwardedClientIP(chain, trusted); err == nil {
		t.Fatal("forwardedClientIP returned nil error for all-trusted chain")
	}
}
