package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFallbackRoutePattern(t *testing.T) {
	noteID := NewNoteID().String()
	tests := []struct {
		name string
		req  *http.Request
		want string
	}{
		{name: "create view", req: httptest.NewRequest(http.MethodGet, "/", nil), want: "GET /{app...}"},
		{name: "legacy create post", req: httptest.NewRequest(http.MethodPost, "/note/"+noteID, nil), want: "POST /{app...}"},
		{name: "config API", req: httptest.NewRequest(http.MethodGet, "/api/config", nil), want: "GET /api/config"},
		{name: "ticket API", req: httptest.NewRequest(http.MethodPost, "/api/tickets", nil), want: "POST /api/tickets"},
		{name: "create API", req: httptest.NewRequest(http.MethodPost, "/api/notes/"+noteID, nil), want: "POST /api/notes/{id}"},
		{name: "preview", req: httptest.NewRequest(http.MethodGet, "/note/"+noteID, nil), want: "GET /{app...}"},
		{name: "open API", req: httptest.NewRequest(http.MethodPost, "/api/notes/"+noteID+"/open", nil), want: "POST /api/notes/{id}/open"},
		{name: "health", req: httptest.NewRequest(http.MethodGet, "/healthz", nil), want: "GET /healthz"},
		{name: "static", req: httptest.NewRequest(http.MethodGet, "/static/test-assets/site.css", nil), want: "GET /static/{version}/{asset...}"},
		{name: "favicon", req: httptest.NewRequest(http.MethodGet, "/favicon.ico", nil), want: "GET /favicon.ico"},
		{name: "well known", req: httptest.NewRequest(http.MethodGet, "/.well-known/security.txt", nil), want: "GET /.well-known/{asset...}"},
		{name: "delete", req: httptest.NewRequest(http.MethodDelete, "/note/"+noteID, nil), want: "DELETE /{app...}"},
		{name: "nested note path", req: httptest.NewRequest(http.MethodGet, "/note/"+noteID+"/extra", nil), want: "GET /{app...}"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := routePattern(tt.req); got != tt.want {
				t.Fatalf("routePattern = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRoutePatternPrefersMatchedServeMuxPattern(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/static/test-assets", nil)
	req.Pattern = "GET /static/{asset}"

	if got := routePattern(req); got != "GET /static/{asset}" {
		t.Fatalf("routePattern = %q, want matched pattern", got)
	}
}

func TestWantsJSON(t *testing.T) {
	noteID := NewNoteID().String()
	tests := []struct {
		name string
		req  *http.Request
		want bool
	}{
		{name: "ticket API", req: httptest.NewRequest(http.MethodPost, "/api/tickets", nil), want: true},
		{name: "config API", req: httptest.NewRequest(http.MethodGet, "/api/config", nil), want: true},
		{name: "create API", req: httptest.NewRequest(http.MethodPost, "/api/notes/"+noteID, nil), want: true},
		{name: "open API", req: httptest.NewRequest(http.MethodPost, "/api/notes/"+noteID+"/open", nil), want: true},
		{name: "nested note API", req: httptest.NewRequest(http.MethodPost, "/api/notes/"+noteID+"/extra", nil), want: true},
		{name: "legacy create post", req: httptest.NewRequest(http.MethodPost, "/note/"+noteID, nil), want: false},
		{name: "preview", req: httptest.NewRequest(http.MethodGet, "/note/"+noteID, nil), want: false},
		{name: "accept header", req: requestWithAcceptJSON(http.MethodGet, "/note/"+noteID), want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := wantsJSON(tt.req); got != tt.want {
				t.Fatalf("wantsJSON = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsStaticAssetRequest(t *testing.T) {
	tests := []struct {
		name string
		req  *http.Request
		want bool
	}{
		{name: "static", req: httptest.NewRequest(http.MethodGet, "/static/test-assets/site.css", nil), want: true},
		{name: "favicon", req: httptest.NewRequest(http.MethodGet, "/favicon.ico", nil), want: true},
		{name: "well known", req: httptest.NewRequest(http.MethodGet, "/.well-known/security.txt", nil), want: true},
		{name: "post static", req: httptest.NewRequest(http.MethodPost, "/static/test-assets/site.css", nil), want: false},
		{name: "create view", req: httptest.NewRequest(http.MethodGet, "/", nil), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isStaticAssetRequest(tt.req); got != tt.want {
				t.Fatalf("isStaticAssetRequest = %v, want %v", got, tt.want)
			}
		})
	}
}

func requestWithAcceptJSON(method, path string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Accept", "text/html, application/json")
	return req
}
