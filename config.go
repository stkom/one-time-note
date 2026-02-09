package main

import (
	"errors"
	"fmt"
	"math"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	envCleanupInterval    = "NOTE_CLEANUP_INTERVAL"
	envDBPath             = "NOTE_DB_PATH"
	envDisplayName        = "NOTE_DISPLAY_NAME"
	envEnvironment        = "NOTE_ENVIRONMENT"
	envFooterText         = "NOTE_FOOTER_TEXT"
	envGracePeriod        = "NOTE_GRACE_PERIOD"
	envHealthCheckSources = "NOTE_HEALTH_CHECK_SOURCES"
	envHideGitHubLink     = "NOTE_HIDE_GITHUB_LINK"
	envHost               = "NOTE_HOST"
	envLegalNoticeURL     = "NOTE_LEGAL_NOTICE_URL"
	envMaxDBSize          = "NOTE_MAX_DB_SIZE"
	envMaxNoteSize        = "NOTE_MAX_NOTE_SIZE"
	envPort               = "NOTE_PORT"
	envPrivacyURL         = "NOTE_PRIVACY_URL"
	envPublicOrigin       = "NOTE_PUBLIC_ORIGIN"
	envRateLimit          = "NOTE_RATE_LIMIT"
	envTermsURL           = "NOTE_TERMS_URL"
	envTrustedProxies     = "NOTE_TRUSTED_PROXIES"
)

const (
	environmentDevelopment = "development"
	environmentProduction  = "production"

	defaultDisplayName = "One Time Note"
	defaultGitHubURL   = "https://github.com/stkom/one-time-note"
	defaultMaxDBSize   = 1 * 1024 * 1024 * 1024
	defaultMaxNoteSize = 1 * 1024
	hardMaxDBSize      = 10 * 1024 * 1024 * 1024
	hardMaxNoteSize    = 10 * 1024 * 1024
)

type StartupOptions struct {
	Development bool
}

type Config struct {
	Brand              Brand
	CleanupInterval    time.Duration
	DBPath             string
	Environment        string
	GracePeriod        time.Duration
	HealthCheckSources []netip.Prefix
	Host               string
	IsDevelopment      bool
	MaxNoteSize        int
	MaxDBSize          int
	Port               string
	PublicOrigin       string
	PublicOriginHost   string
	RateLimit          RateSpec
	TrustedProxies     []netip.Prefix
}

type Brand struct {
	DisplayName string
	FooterText  string
	GitHubURL   string
	LegalLinks  []LegalLink
}

type LegalLink struct {
	Label string
	URL   string
}

type RateSpec struct {
	Requests int
	Window   time.Duration
	Burst    int
}

func NewConfig(getenv func(string) string) (*Config, error) {
	return NewConfigWithOptions(getenv, StartupOptions{})
}

func NewConfigWithOptions(getenv func(string) string, options StartupOptions) (*Config, error) {
	if getenv == nil {
		return nil, errors.New("getenv function cannot be nil")
	}

	env := strings.ToLower(strings.TrimSpace(getenv(envEnvironment)))
	if env == "" {
		if options.Development {
			env = environmentDevelopment
		} else {
			env = environmentProduction
		}
	}
	if env != environmentProduction && env != environmentDevelopment {
		return nil, fmt.Errorf("%s must be %q or %q", envEnvironment, environmentProduction, environmentDevelopment)
	}
	if options.Development && env == environmentProduction {
		return nil, fmt.Errorf("--dev cannot be used with %s=production", envEnvironment)
	}
	if !options.Development && env == environmentDevelopment {
		return nil, fmt.Errorf("%s=development requires --dev", envEnvironment)
	}

	cfg := Config{
		Brand:           Brand{DisplayName: defaultDisplayName, GitHubURL: defaultGitHubURL},
		Environment:     env,
		IsDevelopment:   env == environmentDevelopment,
		Host:            "127.0.0.1",
		Port:            "8080",
		DBPath:          "data.db",
		CleanupInterval: 1 * time.Hour,
		GracePeriod:     3 * time.Second,
		MaxNoteSize:     defaultMaxNoteSize,
		MaxDBSize:       defaultMaxDBSize,
		RateLimit:       RateSpec{Requests: 60, Window: time.Minute, Burst: 120},
	}

	if tmp := strings.TrimSpace(getenv(envHost)); tmp != "" {
		cfg.Host = tmp
	}
	if tmp := strings.TrimSpace(getenv(envPort)); tmp != "" {
		cfg.Port = tmp
	}
	if tmp := strings.TrimSpace(getenv(envDBPath)); tmp != "" {
		cfg.DBPath = tmp
	}
	if tmp := strings.TrimSpace(getenv(envDisplayName)); tmp != "" {
		cfg.Brand.DisplayName = tmp
	}
	if tmp := strings.TrimSpace(getenv(envFooterText)); tmp != "" {
		cfg.Brand.FooterText = tmp
	}
	if hideGitHubLink, err := parseOptionalBool(getenv(envHideGitHubLink), false); err != nil {
		return nil, fmt.Errorf("error parsing %s: %w", envHideGitHubLink, err)
	} else if hideGitHubLink {
		cfg.Brand.GitHubURL = ""
	}
	if legalLinks, parseErr := parseLegalLinks(getenv); parseErr != nil {
		return nil, parseErr
	} else {
		cfg.Brand.LegalLinks = legalLinks
	}

	var err error
	if cfg.GracePeriod, err = parseOptionalDuration(getenv, envGracePeriod, cfg.GracePeriod, 0, 60*time.Second); err != nil {
		return nil, err
	}
	if cfg.CleanupInterval, err = parseOptionalDuration(getenv, envCleanupInterval, cfg.CleanupInterval, time.Minute, 24*time.Hour); err != nil {
		return nil, err
	}
	if cfg.MaxNoteSize, err = parseOptionalByteSizeInt(getenv, envMaxNoteSize, cfg.MaxNoteSize, 1, hardMaxNoteSize); err != nil {
		return nil, err
	}
	if cfg.MaxDBSize, err = parseOptionalByteSizeInt(getenv, envMaxDBSize, cfg.MaxDBSize, 1*1024*1024, hardMaxDBSize); err != nil {
		return nil, err
	}

	if tmp := strings.TrimSpace(getenv(envPublicOrigin)); tmp != "" {
		cfg.PublicOrigin, cfg.PublicOriginHost, err = parsePublicOrigin(tmp, cfg.IsDevelopment)
		if err != nil {
			return nil, err
		}
	}

	if tmp := strings.TrimSpace(getenv(envTrustedProxies)); tmp != "" {
		if cfg.TrustedProxies, err = parseOptionalPrefixes(tmp); err != nil {
			return nil, fmt.Errorf("error parsing %s: %w", envTrustedProxies, err)
		}
	} else if !cfg.IsDevelopment {
		cfg.TrustedProxies = defaultTrustedProxyPrefixes()
	}

	cfg.HealthCheckSources = loopbackPrefixes()
	if !cfg.IsDevelopment {
		cfg.HealthCheckSources = append(cfg.HealthCheckSources, cfg.TrustedProxies...)
	}
	if tmp := strings.TrimSpace(getenv(envHealthCheckSources)); tmp != "" {
		configured, err := parseOptionalPrefixes(tmp)
		if err != nil {
			return nil, fmt.Errorf("error parsing %s: %w", envHealthCheckSources, err)
		}
		cfg.HealthCheckSources = append(cfg.HealthCheckSources, configured...)
	}

	if cfg.RateLimit, err = parseOptionalRate(getenv(envRateLimit), cfg.RateLimit); err != nil {
		return nil, fmt.Errorf("error parsing %s: %w", envRateLimit, err)
	}

	return &cfg, nil
}

func parseLegalLinks(getenv func(string) string) ([]LegalLink, error) {
	specs := []struct {
		name  string
		label string
	}{
		{name: envPrivacyURL, label: "Privacy"},
		{name: envTermsURL, label: "Terms"},
		{name: envLegalNoticeURL, label: "Legal notice"},
	}

	links := make([]LegalLink, 0, len(specs))
	for _, spec := range specs {
		raw := strings.TrimSpace(getenv(spec.name))
		if raw == "" {
			continue
		}

		linkURL, err := parseLegalLinkURL(spec.name, raw)
		if err != nil {
			return nil, err
		}
		links = append(links, LegalLink{Label: spec.label, URL: linkURL})
	}
	return links, nil
}

func parseLegalLinkURL(name string, raw string) (string, error) {
	if strings.ContainsAny(raw, " \t\r\n") {
		return "", fmt.Errorf("%s must not contain whitespace", name)
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("error parsing %s: %w", name, err)
	}

	if parsed.IsAbs() {
		scheme := strings.ToLower(parsed.Scheme)
		if (scheme == "http" || scheme == "https") && parsed.Host != "" {
			return raw, nil
		}
		return "", fmt.Errorf("%s must be an http URL, https URL, or root-relative path", name)
	}

	if strings.HasPrefix(raw, "/") && !strings.HasPrefix(raw, "//") {
		return raw, nil
	}
	return "", fmt.Errorf("%s must be an http URL, https URL, or root-relative path", name)
}

func parseOptionalBool(raw string, defaultValue bool) (bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultValue, nil
	}

	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, err
	}
	return value, nil
}

func defaultTrustedProxyPrefixes() []netip.Prefix {
	return []netip.Prefix{
		netip.MustParsePrefix("127.0.0.0/8"),
		netip.MustParsePrefix("::1/128"),
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("172.16.0.0/12"),
		netip.MustParsePrefix("192.168.0.0/16"),
		netip.MustParsePrefix("169.254.0.0/16"),
		netip.MustParsePrefix("fe80::/10"),
		netip.MustParsePrefix("fc00::/7"),
	}
}

func parseOptionalDuration(getenv func(string) string, name string, defaultValue, minValue, maxValue time.Duration) (time.Duration, error) {
	tmp := strings.TrimSpace(getenv(name))
	if tmp == "" {
		return defaultValue, nil
	}

	duration, err := time.ParseDuration(tmp)
	if err != nil {
		return 0, fmt.Errorf("error parsing %s: %w", name, err)
	}
	if duration < minValue || duration > maxValue {
		return 0, fmt.Errorf("%s must be between %s and %s", name, minValue, maxValue)
	}
	return duration, nil
}

func parseOptionalByteSizeInt(getenv func(string) string, name string, defaultValue, minValue, maxValue int) (int, error) {
	value, err := parseOptionalByteSize(getenv, name, int64(defaultValue), int64(minValue), int64(maxValue))
	if err != nil {
		return 0, err
	}
	return int(value), nil
}

func parseOptionalByteSize(getenv func(string) string, name string, defaultValue, minValue, maxValue int64) (int64, error) {
	tmp := strings.TrimSpace(getenv(name))
	if tmp == "" {
		return defaultValue, nil
	}

	value, err := parseByteSize(tmp)
	if err != nil {
		return 0, fmt.Errorf("error parsing %s: %w", name, err)
	}
	if value < minValue || value > maxValue {
		return 0, fmt.Errorf("%s must be between %d and %d bytes", name, minValue, maxValue)
	}
	return value, nil
}

func parseByteSize(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, errors.New("value is empty")
	}

	lower := strings.ToLower(value)
	multiplier := int64(1)
	for _, suffix := range []struct {
		text       string
		multiplier int64
	}{
		{"gib", 1024 * 1024 * 1024},
		{"gb", 1000 * 1000 * 1000},
		{"mib", 1024 * 1024},
		{"mb", 1000 * 1000},
		{"kib", 1024},
		{"kb", 1000},
		{"b", 1},
	} {
		if strings.HasSuffix(lower, suffix.text) {
			multiplier = suffix.multiplier
			value = strings.TrimSpace(value[:len(value)-len(suffix.text)])
			break
		}
	}
	if value == "" {
		return 0, errors.New("missing numeric value")
	}
	amount, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, err
	}
	if amount < 0 {
		return 0, errors.New("value must be positive")
	}
	if amount > math.MaxInt64/multiplier {
		return 0, errors.New("value is too large")
	}
	return amount * multiplier, nil
}

func parsePublicOrigin(raw string, isDevelopment bool) (string, string, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", "", fmt.Errorf("error parsing %s: %w", envPublicOrigin, err)
	}
	if parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", "", fmt.Errorf("%s must be an absolute origin URL", envPublicOrigin)
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return "", "", fmt.Errorf("%s must not include a path", envPublicOrigin)
	}
	if !isDevelopment && parsed.Scheme != "https" {
		return "", "", fmt.Errorf("%s must use https in production", envPublicOrigin)
	}
	origin := parsed.Scheme + "://" + parsed.Host
	return origin, parsed.Host, nil
}

func parseOptionalPrefixes(raw string) ([]netip.Prefix, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	parts := strings.Split(raw, ",")
	prefixes := make([]netip.Prefix, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.Contains(part, "/") {
			prefix, err := netip.ParsePrefix(part)
			if err != nil {
				return nil, err
			}
			prefixes = append(prefixes, prefix.Masked())
			continue
		}
		addr, err := netip.ParseAddr(part)
		if err != nil {
			return nil, err
		}
		prefixes = append(prefixes, netip.PrefixFrom(addr, addr.BitLen()))
	}
	return prefixes, nil
}

func loopbackPrefixes() []netip.Prefix {
	return []netip.Prefix{
		netip.MustParsePrefix("127.0.0.0/8"),
		netip.MustParsePrefix("::1/128"),
	}
}

func parseOptionalRate(raw string, defaultValue RateSpec) (RateSpec, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultValue, nil
	}
	parts := strings.Split(raw, ",")
	if len(parts) != 2 {
		return RateSpec{}, errors.New("expected rate format requests/window,burst")
	}
	ratePart := strings.TrimSpace(parts[0])
	burstPart := strings.TrimSpace(parts[1])

	ratePieces := strings.Split(ratePart, "/")
	if len(ratePieces) != 2 {
		return RateSpec{}, errors.New("expected rate format requests/window")
	}
	requests, err := strconv.Atoi(strings.TrimSpace(ratePieces[0]))
	if err != nil {
		return RateSpec{}, err
	}
	window, err := parseRateWindow(strings.TrimSpace(ratePieces[1]))
	if err != nil {
		return RateSpec{}, err
	}
	burst, err := strconv.Atoi(burstPart)
	if err != nil {
		return RateSpec{}, err
	}

	spec := RateSpec{Requests: requests, Window: window, Burst: burst}
	if err := validateRateSpec(spec); err != nil {
		return RateSpec{}, err
	}
	return spec, nil
}

func parseRateWindow(value string) (time.Duration, error) {
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, err
	}
	if duration <= 0 {
		return 0, errors.New("window must be positive")
	}
	return duration, nil
}

func validateRateSpec(spec RateSpec) error {
	if spec.Requests <= 0 || spec.Burst <= 0 || spec.Window <= 0 {
		return errors.New("rate and burst must be positive")
	}
	perMinute := float64(spec.Requests) * float64(time.Minute) / float64(spec.Window)
	if perMinute < 1 || perMinute > 1000 {
		return errors.New("rate limit must be between 1/minute and 1000/minute")
	}
	if spec.Burst < 1 || spec.Burst > 5000 {
		return errors.New("burst must be between 1 and 5000")
	}
	return nil
}
