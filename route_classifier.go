package main

import (
	"net/http"
	"strings"
)

func routePattern(r *http.Request) string {
	if r == nil {
		return "unmatched"
	}
	if r.Pattern != "" {
		return r.Pattern
	}
	if r.URL == nil {
		return "unmatched"
	}
	return fallbackRoutePattern(r.Method, r.URL.Path)
}

func fallbackRoutePattern(method, path string) string {
	switch {
	case method == http.MethodGet && path == "/healthz":
		return "GET /healthz"
	case method == http.MethodGet && strings.HasPrefix(path, "/static/"):
		return "GET /static/{version}/{asset...}"
	case method == http.MethodGet && path == "/favicon.ico":
		return "GET /favicon.ico"
	case method == http.MethodGet && strings.HasPrefix(path, "/.well-known/"):
		return "GET /.well-known/{asset...}"
	case method == http.MethodGet && path == "/api/config":
		return "GET /api/config"
	case method == http.MethodPost && path == "/api/tickets":
		return "POST /api/tickets"
	case method == http.MethodPost && strings.HasPrefix(path, "/api/notes/") && strings.HasSuffix(path, "/open"):
		return "POST /api/notes/{id}/open"
	case method == http.MethodPost && strings.HasPrefix(path, "/api/notes/"):
		return "POST /api/notes/{id}"
	default:
		return method + " /{app...}"
	}
}

func isJSONRoute(r *http.Request) bool {
	if r == nil || r.URL == nil {
		return false
	}
	return strings.HasPrefix(r.URL.Path, "/api/")
}

func isStaticAssetRequest(r *http.Request) bool {
	if r == nil || r.URL == nil {
		return false
	}
	return r.Method == http.MethodGet &&
		(strings.HasPrefix(r.URL.Path, "/static/") ||
			r.URL.Path == "/favicon.ico" ||
			strings.HasPrefix(r.URL.Path, "/.well-known/"))
}

func isSuccessfulStaticRoute(route string, code int) bool {
	return strings.HasPrefix(route, "GET /static/") && code < http.StatusBadRequest
}
