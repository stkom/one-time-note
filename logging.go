package main

import (
	"log/slog"
	"net/http"
)

const (
	logMsgDatabasePerms   = "database_permissions"
	logMsgHTTPRequest     = "http_request"
	logMsgKeyManagement   = "key_management"
	logMsgRequestError    = "request_error"
	logMsgSecurityEvent   = "security_event"
	logMsgServerError     = "server_error"
	logMsgServerLifecycle = "server_lifecycle"
	logMsgStartupFailed   = "startup_failed"
	logMsgStartupSummary  = "startup_summary"
)

func logSecurityEvent(event string, attrs ...any) {
	args := []any{"event", event}
	args = append(args, attrs...)
	slog.Info(logMsgSecurityEvent, args...)
}

func logRequestError(r *http.Request, event string, err error, attrs ...any) {
	args := []any{"event", event, "error", err}
	if r != nil {
		args = append(args, "route", routePattern(r))
		if ctx, ok := r.Context().Value(httpContextKey).(*HTTPContext); ok {
			args = append(args, "client", ctx.ClientKey)
		}
	}
	args = append(args, attrs...)
	slog.Error(logMsgRequestError, args...)
}

func logServerError(event string, err error, attrs ...any) {
	args := []any{"event", event, "error", err}
	args = append(args, attrs...)
	slog.Error(logMsgServerError, args...)
}

func logStartupSummary(cfg *Config, addr string) {
	slog.Info(logMsgStartupSummary,
		"event", "startup_completed",
		"environment", cfg.Environment,
		"addr", addr,
		"db_path", cfg.DBPath,
		"public_origin_host", cfg.PublicOriginHost,
		"trusted_proxy_count", len(cfg.TrustedProxies),
		"max_note_size", cfg.MaxNoteSize,
		"max_db_size", cfg.MaxDBSize,
		"rate_limits_enabled", true,
	)
}
