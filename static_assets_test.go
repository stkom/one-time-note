package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"testing/fstest"
)

func TestNewAssetVersionChangesWhenContentChanges(t *testing.T) {
	first := fstest.MapFS{
		"site.css":       {Data: []byte("body { color: black; }")},
		"create-note.js": {Data: []byte("console.log('first');")},
	}
	second := fstest.MapFS{
		"site.css":       {Data: []byte("body { color: white; }")},
		"create-note.js": {Data: []byte("console.log('first');")},
	}

	firstVersion, err := NewAssetVersion(first)
	if err != nil {
		t.Fatalf("NewAssetVersion(first) returned error: %v", err)
	}
	secondVersion, err := NewAssetVersion(second)
	if err != nil {
		t.Fatalf("NewAssetVersion(second) returned error: %v", err)
	}

	if firstVersion == secondVersion {
		t.Fatalf("NewAssetVersion returned %q for different static content", firstVersion)
	}
	if len(firstVersion) != 16 {
		t.Fatalf("NewAssetVersion length = %d, want 16", len(firstVersion))
	}
}

func TestHandleStaticAssetsServesImmutableVersionedAssets(t *testing.T) {
	const version = "assets123"
	staticFS := fstest.MapFS{
		"site.css": {Data: []byte("body { color: black; }")},
	}
	req := httptest.NewRequest(http.MethodGet, "/static/assets123/site.css", nil)
	req.SetPathValue("version", version)
	req.SetPathValue("asset", "site.css")
	rec := httptest.NewRecorder()

	HandleStaticAssets(version, staticFS).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Cache-Control"); got != immutableStaticCacheControl {
		t.Fatalf("Cache-Control = %q, want %q", got, immutableStaticCacheControl)
	}
	if got := rec.Body.String(); got != "body { color: black; }" {
		t.Fatalf("body = %q", got)
	}
}

func TestHandleStaticAssetsRejectsStaleVersion(t *testing.T) {
	staticFS := fstest.MapFS{
		"site.css": {Data: []byte("body { color: black; }")},
	}
	req := httptest.NewRequest(http.MethodGet, "/static/old/site.css", nil)
	req.SetPathValue("version", "old")
	req.SetPathValue("asset", "site.css")
	rec := httptest.NewRecorder()

	HandleStaticAssets("current", staticFS).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestServerHandlerDoesNotServeUnversionedStaticAssets(t *testing.T) {
	cfg, err := NewConfigWithOptions(envMap(map[string]string{}), StartupOptions{Development: true})
	if err != nil {
		t.Fatalf("NewConfigWithOptions returned error: %v", err)
	}
	app := &app{
		Cfg:          cfg,
		Views:        &Views{FS: os.DirFS("web/html"), AssetVersion: "assets123"},
		StaticFS:     fstest.MapFS{"site.css": {Data: []byte("body {}")}},
		AssetVersion: "assets123",
	}
	req := httptest.NewRequest(http.MethodGet, "/static/site.css", nil)
	rec := httptest.NewRecorder()

	newServerHandler(app).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if strings.Contains(rec.Body.String(), "body {}") {
		t.Fatal("unversioned static path served static content")
	}
}
