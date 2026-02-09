package main

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSecurityHeadersMiddlewareSecurityHeaders(t *testing.T) {
	cfg := mustTestConfig(t, map[string]string{
		envPublicOrigin:   "https://notes.example.test",
		envTrustedProxies: "10.0.0.1",
	})
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	SecurityHeadersMiddleware(cfg, next).ServeHTTP(rec, req)

	wantCSP := "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self'; connect-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'; frame-src 'none'; object-src 'none'; script-src-attr 'none'; require-trusted-types-for 'script'; upgrade-insecure-requests"
	if got := rec.Header().Get("Content-Security-Policy"); got != wantCSP {
		t.Fatalf("Content-Security-Policy = %q, want %q", got, wantCSP)
	}
	if got := rec.Header().Get("Strict-Transport-Security"); got != "max-age=31536000" {
		t.Fatalf("Strict-Transport-Security = %q, want max-age=31536000", got)
	}
	if got := rec.Header().Get("Permissions-Policy"); !strings.Contains(got, "camera=()") || !strings.Contains(got, "display-capture=()") {
		t.Fatalf("Permissions-Policy = %q, want deny-by-default features", got)
	}
	if got := rec.Header().Get("Cross-Origin-Opener-Policy"); got != "same-origin" {
		t.Fatalf("Cross-Origin-Opener-Policy = %q, want same-origin", got)
	}
	if got := rec.Header().Get("Cross-Origin-Resource-Policy"); got != "same-origin" {
		t.Fatalf("Cross-Origin-Resource-Policy = %q, want same-origin", got)
	}
	if got := rec.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Fatalf("Referrer-Policy = %q, want no-referrer", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := rec.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Fatalf("X-Frame-Options = %q, want DENY", got)
	}
}

func TestSecurityHeadersMiddlewareDevelopmentOmitsProductionOnlyDirectives(t *testing.T) {
	cfg, err := NewConfigWithOptions(envMap(map[string]string{}), StartupOptions{Development: true})
	if err != nil {
		t.Fatalf("NewConfigWithOptions returned error: %v", err)
	}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	SecurityHeadersMiddleware(cfg, next).ServeHTTP(rec, req)

	if got := rec.Header().Get("Strict-Transport-Security"); got != "" {
		t.Fatalf("Strict-Transport-Security = %q, want empty in development", got)
	}
	if got := rec.Header().Get("Content-Security-Policy"); strings.Contains(got, "upgrade-insecure-requests") {
		t.Fatalf("development CSP contains upgrade-insecure-requests: %q", got)
	}
}

func TestTrustedProxyMiddlewareValidatesForwardedHTTPSAndClientIP(t *testing.T) {
	cfg := mustTestConfig(t, map[string]string{
		envPublicOrigin:   "https://notes.example.test",
		envTrustedProxies: "10.0.0.0/24",
	})
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := GetHTTPContext(r)
		if ctx.ClientKey != "203.0.113.10" {
			t.Fatalf("ClientKey = %q, want 203.0.113.10", ctx.ClientKey)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.8:12345"
	req.Header.Set("X-Forwarded-For", "203.0.113.10, 10.0.0.9")
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "notes.example.test")
	rec := httptest.NewRecorder()

	HTTPContextMiddleware(TrustedProxyMiddleware(cfg, next)).ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
}

func TestTrustedProxyMiddlewareDerivesPublicOriginFromForwardedHost(t *testing.T) {
	cfg := mustTestConfig(t, map[string]string{})
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := GetHTTPContext(r)
		if ctx.ClientKey != "203.0.113.10" {
			t.Fatalf("ClientKey = %q, want 203.0.113.10", ctx.ClientKey)
		}
		if ctx.PublicOrigin != "https://notes.example.test" {
			t.Fatalf("PublicOrigin = %q, want https://notes.example.test", ctx.PublicOrigin)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "172.16.0.8:12345"
	req.Header.Set("X-Forwarded-For", "203.0.113.10, 172.16.0.8")
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "notes.example.test")
	rec := httptest.NewRecorder()

	HTTPContextMiddleware(TrustedProxyMiddleware(cfg, next)).ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
}

func TestTrustedProxyMiddlewareConfiguredPublicOriginPinsForwardedHost(t *testing.T) {
	cfg := mustTestConfig(t, map[string]string{
		envPublicOrigin: "https://notes.example.test",
	})
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next handler should not run")
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "172.16.0.8:12345"
	req.Header.Set("X-Forwarded-For", "203.0.113.10, 172.16.0.8")
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "other.example.test")
	rec := httptest.NewRecorder()

	HTTPContextMiddleware(TrustedProxyMiddleware(cfg, next)).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestTrustedProxyMiddlewareRejectsUntrustedProductionSource(t *testing.T) {
	cfg := mustTestConfig(t, map[string]string{
		envPublicOrigin:   "https://notes.example.test",
		envTrustedProxies: "10.0.0.0/24",
	})
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next handler should not run")
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.10:12345"
	rec := httptest.NewRecorder()

	HTTPContextMiddleware(TrustedProxyMiddleware(cfg, next)).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestLoggingMiddlewareRedactsRawRequestURI(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	req := httptest.NewRequest(http.MethodGet, "/note/secret-id?ticket=secret-ticket", nil)
	req.Pattern = "GET /note/{id}"
	rec := httptest.NewRecorder()

	logged := captureLogs(t, func() {
		HTTPContextMiddleware(LoggingMiddleware(next)).ServeHTTP(rec, req)
	})

	assertLogOmits(t, logged, "secret-id", "secret-ticket", "/note/secret-id")
	if !strings.Contains(logged, "GET /note/{id}") {
		t.Fatalf("log does not contain route pattern: %s", logged)
	}
	if !strings.Contains(logged, "event=request_completed") {
		t.Fatalf("log does not contain request completion event: %s", logged)
	}
}

func TestInvalidTicketSecurityLogUsesSafeFields(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	ticket, err := app.NoteService.CreateTicket()
	if err != nil {
		t.Fatalf("CreateTicket returned error: %v", err)
	}
	pathID := NewNoteID()
	body := createNoteRequest{
		Ticket:           ticket.Value,
		BurnToken:        &ticket.BurnToken,
		Payload:          testPayload("secret"),
		ExpiresInSeconds: int(testNoteLifetime / time.Second),
	}

	logged := captureLogs(t, func() {
		rec := performJSONRequest(t, app, http.MethodPost, "/api/notes/"+pathID.String(), body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d body = %s, want %d", rec.Code, rec.Body.String(), http.StatusBadRequest)
		}
	})

	assertLogOmits(t, logged, pathID.String(), ticket.Data.ID.String(), ticket.Value, ticket.BurnToken.String())
	for _, want := range []string{"msg=security_event", "event=invalid_ticket", "route=\"POST /api/notes/{id}\"", "reason=invalid", "status=400"} {
		if !strings.Contains(logged, want) {
			t.Fatalf("log does not contain %q: %s", want, logged)
		}
	}
}

func TestOpenFailureSecurityLogUsesSafeFields(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	ticket, err := app.NoteService.CreateTicket()
	if err != nil {
		t.Fatalf("CreateTicket returned error: %v", err)
	}
	if _, err := app.NoteService.CreateNote(ticket.Data.ID, ticket.Value, ticket.BurnToken, testPayloadBytes("secret"), testNoteLifetime); err != nil {
		t.Fatalf("CreateNote returned error: %v", err)
	}
	wrongToken := NewBurnToken()

	logged := captureLogs(t, func() {
		rec := performJSONRequest(t, app, http.MethodPost, "/api/notes/"+ticket.Data.ID.String()+"/open", openNoteRequest{BurnToken: &wrongToken})
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d body = %s, want %d", rec.Code, rec.Body.String(), http.StatusNotFound)
		}
	})

	assertLogOmits(t, logged, ticket.Data.ID.String(), ticket.Value, ticket.BurnToken.String(), wrongToken.String())
	for _, want := range []string{"msg=security_event", "event=open_failed", "route=\"POST /api/notes/{id}/open\"", "reason=open_failed", "status=404"} {
		if !strings.Contains(logged, want) {
			t.Fatalf("log does not contain %q: %s", want, logged)
		}
	}
	if strings.Contains(logged, "note_key=") {
		t.Fatalf("log contains removed note_key field: %s", logged)
	}
}

func TestRateLimitMiddlewareUsesSharedAppLimit(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	app.RateLimiter = NewRateLimiter(RateSpec{Requests: 1, Window: time.Minute, Burst: 1})

	logged := captureLogs(t, func() {
		first := performRequest(app, http.MethodGet, "/", nil)
		if first.Code != http.StatusOK {
			t.Fatalf("first status = %d body = %s, want %d", first.Code, first.Body.String(), http.StatusOK)
		}
		second := performRequest(app, http.MethodGet, "/api/config", nil)
		if second.Code != http.StatusTooManyRequests {
			t.Fatalf("second status = %d body = %s, want %d", second.Code, second.Body.String(), http.StatusTooManyRequests)
		}
		if got := second.Header().Get("Retry-After"); got == "" {
			t.Fatal("Retry-After header was empty")
		}
	})

	for _, want := range []string{"event=rate_limited", "route=\"GET /api/config\"", "status=429"} {
		if !strings.Contains(logged, want) {
			t.Fatalf("log does not contain %q: %s", want, logged)
		}
	}
}

func TestRateLimitMiddlewareBypassesHealthAndAssets(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{name: "health", path: "/healthz"},
		{name: "static", path: "/static/test-assets/site.css"},
		{name: "favicon", path: "/favicon.ico"},
		{name: "well known", path: "/.well-known/security.txt"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app, cleanup := setupTestApp(t)
			defer cleanup()
			app.RateLimiter = NewRateLimiter(RateSpec{Requests: 1, Window: time.Minute, Burst: 1})

			first := performRequest(app, http.MethodGet, "/", nil)
			if first.Code != http.StatusOK {
				t.Fatalf("first status = %d body = %s, want %d", first.Code, first.Body.String(), http.StatusOK)
			}
			second := performRequest(app, http.MethodGet, tt.path, nil)
			if second.Code == http.StatusTooManyRequests {
				t.Fatalf("%s was rate limited: body = %s", tt.path, second.Body.String())
			}
		})
	}
}

func TestProxyRejectionSecurityLogRedactsRawRequestURI(t *testing.T) {
	cfg := mustTestConfig(t, map[string]string{
		envPublicOrigin:   "https://notes.example.test",
		envTrustedProxies: "10.0.0.0/24",
	})
	req := httptest.NewRequest(http.MethodGet, "/note/secret-id?ticket=secret-ticket", nil)
	req.RemoteAddr = "203.0.113.10:12345"
	rec := httptest.NewRecorder()

	logged := captureLogs(t, func() {
		HTTPContextMiddleware(TrustedProxyMiddleware(cfg, http.NotFoundHandler())).ServeHTTP(rec, req)
	})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	assertLogOmits(t, logged, "secret-id", "secret-ticket", "/note/secret-id")
	for _, want := range []string{"msg=security_event", "event=proxy_rejected", "route=\"GET /{app...}\"", "reason=untrusted_proxy_source", "remote=203.0.113.10", "status=400"} {
		if !strings.Contains(logged, want) {
			t.Fatalf("log does not contain %q: %s", want, logged)
		}
	}
}

func TestHealthzBypassesTrustedProxyForLoopback(t *testing.T) {
	cfg := mustTestConfig(t, map[string]string{
		envPublicOrigin:   "https://notes.example.test",
		envTrustedProxies: "10.0.0.0/24",
	})
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if GetHTTPContext(r).ClientKey != "health" {
			t.Fatalf("ClientKey = %q, want health", GetHTTPContext(r).ClientKey)
		}
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()

	HTTPContextMiddleware(TrustedProxyMiddleware(cfg, next)).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func performRequest(app *app, method, path string, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	rec := httptest.NewRecorder()

	newServerHandler(app).ServeHTTP(rec, req)
	return rec
}

func mustTestConfig(t *testing.T, env map[string]string) *Config {
	t.Helper()
	cfg, err := NewConfig(envMap(env))
	if err != nil {
		t.Fatalf("NewConfig returned error: %v", err)
	}
	return cfg
}

func captureLogs(t *testing.T, fn func()) string {
	t.Helper()
	var logBuffer bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuffer, &slog.HandlerOptions{Level: slog.LevelInfo})))
	defer slog.SetDefault(previous)

	fn()
	return logBuffer.String()
}

func assertLogOmits(t *testing.T, logged string, forbidden ...string) {
	t.Helper()
	for _, value := range forbidden {
		if strings.Contains(logged, value) {
			t.Fatalf("log contains sensitive value %q: %s", value, logged)
		}
	}
}
