package main

import (
	"fmt"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestNewConfigProductionDefaultsTrustedProxyRanges(t *testing.T) {
	cfg, err := NewConfig(envMap(map[string]string{}))
	if err != nil {
		t.Fatalf("NewConfig returned error: %v", err)
	}
	if cfg.PublicOrigin != "" || cfg.PublicOriginHost != "" {
		t.Fatalf("unexpected default public origin: %q host %q", cfg.PublicOrigin, cfg.PublicOriginHost)
	}
	for _, raw := range []string{"127.0.0.1", "172.16.0.10", "192.168.1.10", "fd00::1"} {
		addr := netip.MustParseAddr(raw)
		if !prefixContains(cfg.TrustedProxies, addr) {
			t.Fatalf("default trusted proxies do not contain %s: %#v", raw, cfg.TrustedProxies)
		}
		if !prefixContains(cfg.HealthCheckSources, addr) {
			t.Fatalf("default health check sources do not contain %s: %#v", raw, cfg.HealthCheckSources)
		}
	}
}

func TestNewConfigProductionValues(t *testing.T) {
	cfg, err := NewConfig(envMap(map[string]string{
		envPublicOrigin:       "https://notes.example.test/",
		envTrustedProxies:     "10.0.0.1, 192.168.0.0/24",
		envHealthCheckSources: "10.0.1.0/24",
		envPort:               "9090",
		envHost:               "0.0.0.0",
		envDBPath:             "/tmp/one-time-note.db",
		envGracePeriod:        "5s",
		envCleanupInterval:    "30m",
		envMaxNoteSize:        "2MiB",
		envMaxDBSize:          "2GiB",
		envRateLimit:          "30/1m,60",
		envDisplayName:        "Team Notes",
		envFooterText:         "Internal handoff only",
	}))
	if err != nil {
		t.Fatalf("NewConfig returned error: %v", err)
	}
	if cfg.IsDevelopment {
		t.Fatal("production config reported IsDevelopment")
	}
	if cfg.Host != "0.0.0.0" || cfg.Port != "9090" || cfg.DBPath != "/tmp/one-time-note.db" {
		t.Fatalf("unexpected bind/db config: %#v", cfg)
	}
	if cfg.PublicOrigin != "https://notes.example.test" || cfg.PublicOriginHost != "notes.example.test" {
		t.Fatalf("unexpected public origin: %q host %q", cfg.PublicOrigin, cfg.PublicOriginHost)
	}
	if len(cfg.TrustedProxies) != 2 {
		t.Fatalf("trusted proxy count = %d, want 2", len(cfg.TrustedProxies))
	}
	if len(cfg.HealthCheckSources) != 5 {
		t.Fatalf("health source count = %d, want loopback plus trusted proxies plus configured source", len(cfg.HealthCheckSources))
	}
	if cfg.GracePeriod != 5*time.Second || cfg.CleanupInterval != 30*time.Minute {
		t.Fatalf("unexpected durations: grace=%s cleanup=%s", cfg.GracePeriod, cfg.CleanupInterval)
	}
	if cfg.MaxNoteSize != 2*1024*1024 || cfg.MaxDBSize != 2*1024*1024*1024 {
		t.Fatalf("unexpected byte limits: note=%d db=%d", cfg.MaxNoteSize, cfg.MaxDBSize)
	}
	if cfg.RateLimit.Requests != 30 || cfg.RateLimit.Window != time.Minute || cfg.RateLimit.Burst != 60 {
		t.Fatalf("unexpected abuse controls: %#v", cfg)
	}
	if cfg.Brand.DisplayName != "Team Notes" || cfg.Brand.FooterText != "Internal handoff only" || cfg.Brand.GitHubURL != defaultGitHubURL {
		t.Fatalf("unexpected brand config: %#v", cfg.Brand)
	}
}

func TestNewConfigHideGitHubLink(t *testing.T) {
	cfg, err := NewConfig(envMap(map[string]string{
		envHideGitHubLink: "true",
	}))
	if err != nil {
		t.Fatalf("NewConfig returned error: %v", err)
	}
	if cfg.Brand.GitHubURL != "" {
		t.Fatalf("GitHubURL = %q, want hidden", cfg.Brand.GitHubURL)
	}
}

func TestNewConfigLegalLinks(t *testing.T) {
	cfg, err := NewConfig(envMap(map[string]string{
		"NOTE_LINK_1_TITLE": "Privacy Policy",
		"NOTE_LINK_1_URL":   "http://notes.example.test/privacy",
		"NOTE_LINK_2_TITLE": "Terms of Use",
		"NOTE_LINK_2_URL":   "/terms",
		"NOTE_LINK_3_TITLE": "Legal notice",
		"NOTE_LINK_3_URL":   "https://notes.example.test/legal",
	}))
	if err != nil {
		t.Fatalf("NewConfig returned error: %v", err)
	}

	want := []LegalLink{
		{Label: "Privacy Policy", URL: "http://notes.example.test/privacy"},
		{Label: "Terms of Use", URL: "/terms"},
		{Label: "Legal notice", URL: "https://notes.example.test/legal"},
	}
	if len(cfg.Brand.LegalLinks) != len(want) {
		t.Fatalf("LegalLinks length = %d, want %d: %#v", len(cfg.Brand.LegalLinks), len(want), cfg.Brand.LegalLinks)
	}
	for i := range want {
		if cfg.Brand.LegalLinks[i] != want[i] {
			t.Fatalf("LegalLinks[%d] = %#v, want %#v", i, cfg.Brand.LegalLinks[i], want[i])
		}
	}
}

func TestNewConfigTrimsLegalLinkPairs(t *testing.T) {
	cfg, err := NewConfig(envMap(map[string]string{
		"NOTE_LINK_1_TITLE": " Privacy Policy ",
		"NOTE_LINK_1_URL":   " /privacy ",
	}))
	if err != nil {
		t.Fatalf("NewConfig returned error: %v", err)
	}

	want := []LegalLink{{Label: "Privacy Policy", URL: "/privacy"}}
	if len(cfg.Brand.LegalLinks) != len(want) {
		t.Fatalf("LegalLinks length = %d, want %d: %#v", len(cfg.Brand.LegalLinks), len(want), cfg.Brand.LegalLinks)
	}
	if cfg.Brand.LegalLinks[0] != want[0] {
		t.Fatalf("LegalLinks[0] = %#v, want %#v", cfg.Brand.LegalLinks[0], want[0])
	}
}

func TestNewConfigStopsLegalLinksAtFirstEmptyPair(t *testing.T) {
	cfg, err := NewConfig(envMap(map[string]string{
		"NOTE_LINK_1_TITLE": "Privacy",
		"NOTE_LINK_1_URL":   "/privacy",
		"NOTE_LINK_3_TITLE": "Ignored",
		"NOTE_LINK_3_URL":   "/ignored",
	}))
	if err != nil {
		t.Fatalf("NewConfig returned error: %v", err)
	}

	want := []LegalLink{{Label: "Privacy", URL: "/privacy"}}
	if len(cfg.Brand.LegalLinks) != len(want) {
		t.Fatalf("LegalLinks length = %d, want %d: %#v", len(cfg.Brand.LegalLinks), len(want), cfg.Brand.LegalLinks)
	}
	if cfg.Brand.LegalLinks[0] != want[0] {
		t.Fatalf("LegalLinks[0] = %#v, want %#v", cfg.Brand.LegalLinks[0], want[0])
	}
}

func TestNewConfigEmptyFirstLegalLinkPairProducesNoLinks(t *testing.T) {
	cfg, err := NewConfig(envMap(map[string]string{
		"NOTE_LINK_2_TITLE": "Ignored",
		"NOTE_LINK_2_URL":   "/ignored",
	}))
	if err != nil {
		t.Fatalf("NewConfig returned error: %v", err)
	}

	if len(cfg.Brand.LegalLinks) != 0 {
		t.Fatalf("LegalLinks length = %d, want 0: %#v", len(cfg.Brand.LegalLinks), cfg.Brand.LegalLinks)
	}
}

func TestNewConfigRejectsIncompleteLegalLinkPairs(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{
			name: "missing title",
			env: map[string]string{
				"NOTE_LINK_1_URL": "/privacy",
			},
			want: "NOTE_LINK_1_TITLE",
		},
		{
			name: "missing URL",
			env: map[string]string{
				"NOTE_LINK_1_TITLE": "Privacy",
			},
			want: "NOTE_LINK_1_URL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewConfig(envMap(tt.env))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("NewConfig error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestNewConfigRejectsTooManyLegalLinks(t *testing.T) {
	env := map[string]string{}
	for index := 1; index <= maxConfiguredLinks+1; index++ {
		env[linkTitleEnvName(index)] = "Link"
		env[linkURLEnvName(index)] = "/link"
	}

	_, err := NewConfig(envMap(env))
	want := fmt.Sprintf("maximum of %d", maxConfiguredLinks)
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("NewConfig error = %v, want containing %q", err, want)
	}
}

func TestNewConfigDevelopmentMode(t *testing.T) {
	cfg, err := NewConfigWithOptions(envMap(map[string]string{}), StartupOptions{Development: true})
	if err != nil {
		t.Fatalf("NewConfigWithOptions returned error: %v", err)
	}
	if !cfg.IsDevelopment || cfg.Environment != environmentDevelopment {
		t.Fatalf("expected development config, got %#v", cfg)
	}
}

func TestNewConfigRejectsConflictingDevelopmentSignals(t *testing.T) {
	_, err := NewConfigWithOptions(envMap(map[string]string{envEnvironment: environmentProduction}), StartupOptions{Development: true})
	if err == nil || !strings.Contains(err.Error(), "--dev") {
		t.Fatalf("NewConfigWithOptions error = %v, want --dev conflict", err)
	}

	_, err = NewConfig(envMap(map[string]string{envEnvironment: environmentDevelopment}))
	if err == nil || !strings.Contains(err.Error(), "requires --dev") {
		t.Fatalf("NewConfig error = %v, want development requires --dev", err)
	}
}

func TestNewConfigRejectsInvalidBoundsAndOrigins(t *testing.T) {
	base := map[string]string{
		envPublicOrigin:   "https://notes.example.test",
		envTrustedProxies: "10.0.0.1",
	}
	tests := []struct {
		name string
		key  string
		val  string
		want string
	}{
		{name: "public origin must be https", key: envPublicOrigin, val: "http://notes.example.test", want: "https"},
		{name: "invalid trusted proxy", key: envTrustedProxies, val: "not-an-ip", want: envTrustedProxies},
		{name: "cleanup too small", key: envCleanupInterval, val: "30s", want: envCleanupInterval},
		{name: "grace too large", key: envGracePeriod, val: "61s", want: envGracePeriod},
		{name: "note too large", key: envMaxNoteSize, val: "11MiB", want: envMaxNoteSize},
		{name: "note size overflows", key: envMaxNoteSize, val: "9223372036854775807GiB", want: envMaxNoteSize},
		{name: "database too large", key: envMaxDBSize, val: "11GiB", want: envMaxDBSize},
		{name: "database size overflows", key: envMaxDBSize, val: "9223372036854775807GiB", want: envMaxDBSize},
		{name: "rate too high", key: envRateLimit, val: "2000/1m,1", want: envRateLimit},
		{name: "rate window alias", key: envRateLimit, val: "60/m,120", want: envRateLimit},
		{name: "invalid GitHub link visibility", key: envHideGitHubLink, val: "sometimes", want: envHideGitHubLink},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := map[string]string{}
			for k, v := range base {
				env[k] = v
			}
			env[tt.key] = tt.val
			_, err := NewConfig(envMap(env))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("NewConfig error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestNewConfigRejectsInvalidLegalLinkURLs(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{name: "cannot be relative", url: "privacy", want: "NOTE_LINK_1_URL"},
		{name: "cannot be protocol-relative", url: "//notes.example.test/terms", want: "NOTE_LINK_1_URL"},
		{name: "cannot use unsafe scheme", url: "javascript:alert(1)", want: "NOTE_LINK_1_URL"},
		{name: "cannot contain whitespace", url: "/privacy policy", want: "NOTE_LINK_1_URL"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewConfig(envMap(map[string]string{
				"NOTE_LINK_1_TITLE": "Privacy",
				"NOTE_LINK_1_URL":   tt.url,
			}))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("NewConfig error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestNewConfigRejectsNilGetenv(t *testing.T) {
	_, err := NewConfig(nil)
	if err == nil {
		t.Fatal("expected error for nil getenv")
	}
}

func envMap(values map[string]string) func(string) string {
	return func(key string) string {
		return values[key]
	}
}
