package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"time"
)

type contextKey struct {
	name string
}

var httpContextKey = &contextKey{"HTTPContext"}

type HTTPContext struct {
	ClientIP     netip.Addr
	ClientKey    string
	Err          error
	PublicOrigin string
	RequestStart time.Time
	StatusCode   int
}

func GetHTTPContext(r *http.Request) *HTTPContext {
	val := r.Context().Value(httpContextKey)
	if ctx, ok := val.(*HTTPContext); ok {
		return ctx
	}

	panic("HTTPContext not found")
}

func CSRFMiddleware(next http.Handler) http.Handler {
	var csrf = http.NewCrossOriginProtection()
	return csrf.Handler(next)
}

func SecurityHeadersMiddleware(cfg *Config, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		csp := "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self'; connect-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'; frame-src 'none'; object-src 'none'; script-src-attr 'none'; require-trusted-types-for 'script'"
		if !cfg.IsDevelopment {
			csp += "; upgrade-insecure-requests"
			w.Header().Set("Strict-Transport-Security", "max-age=31536000")
		}
		w.Header().Set("Content-Security-Policy", csp)
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=(), fullscreen=(), display-capture=()")
		w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := GetHTTPContext(r)
		lrw := &loggingResponseWriter{ResponseWriter: w, code: &ctx.StatusCode}
		next.ServeHTTP(lrw, r)

		code := ctx.StatusCode
		route := routePattern(r)
		if isSuccessfulStaticRoute(route, code) {
			return
		}

		duration := time.Since(ctx.RequestStart)
		args := []any{
			"event", "request_completed",
			"method", r.Method,
			"route", route,
			"status", code,
			"duration_ms", duration.Milliseconds(),
			"client", ctx.ClientKey,
		}
		if ctx.Err != nil {
			args = append(args, "error", ctx.Err)
		}
		slog.Info(logMsgHTTPRequest, args...)
	})
}

func HTTPContextMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		routeCtx := &HTTPContext{
			RequestStart: time.Now(),
			StatusCode:   http.StatusOK,
			ClientKey:    "unknown",
		}
		r = r.WithContext(context.WithValue(r.Context(), httpContextKey, routeCtx))
		next.ServeHTTP(w, r)
	})
}

func RateLimitMiddleware(limiter *RateLimiter, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isHealthCheckRequest(r) || isStaticAssetRequest(r) || limiter == nil {
			next.ServeHTTP(w, r)
			return
		}

		ctx := GetHTTPContext(r)
		allowed, retryAfter := limiter.AllowRequest(ctx.ClientKey)
		if !allowed {
			w.Header().Set("Retry-After", fmt.Sprintf("%.0f", retryAfter.Seconds()))
			writeErrorResponse(w, r, http.StatusTooManyRequests, "rate_limited")
			logSecurityEvent("rate_limited", "route", routePattern(r), "client", ctx.ClientKey, "status", http.StatusTooManyRequests)
			return
		}

		next.ServeHTTP(w, r)
	})
}

type loggingResponseWriter struct {
	http.ResponseWriter
	code *int
}

func (w *loggingResponseWriter) WriteHeader(code int) {
	w.ResponseWriter.WriteHeader(code)
	*w.code = code
}

func (w *loggingResponseWriter) Write(data []byte) (int, error) {
	if *w.code == 0 {
		*w.code = http.StatusOK
	}
	return w.ResponseWriter.Write(data)
}
