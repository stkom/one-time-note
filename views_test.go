package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"testing/fstest"
)

func TestViewsRenderVersionedAssetPaths(t *testing.T) {
	views, err := NewViews(os.DirFS("web/html"), "assets123", false, Brand{DisplayName: "Test Relay"})
	if err != nil {
		t.Fatalf("NewViews returned error: %v", err)
	}
	cases := []struct {
		name     string
		template string
		model    any
		want     []string
	}{
		{
			name:     "create",
			template: "create",
			model: CreateViewModel{
				Brand:        Brand{DisplayName: "Test Relay"},
				MaxNoteSize:  1048576,
				PublicOrigin: "https://notes.example.test",
			},
			want: []string{
				`href="/static/assets123/favicon.svg"`,
				`href="/static/assets123/site.css"`,
				`src="/static/assets123/create-note.js"`,
				`JavaScript and Web Crypto are required to encrypt notes in this browser.`,
				`value="86400"`,
				`aria-selected="true" data-expires-in-seconds="86400">1 day</button>`,
				`data-expires-in-seconds="604800">7 days</button>`,
			},
		},
		{
			name:     "note",
			template: "note",
			model: NoteViewModel{
				Brand: Brand{DisplayName: "Test Relay"},
				ID:    "note-id",
			},
			want: []string{
				`href="/static/assets123/favicon.svg"`,
				`href="/static/assets123/site.css"`,
				`src="/static/assets123/view-note.js"`,
				`JavaScript and Web Crypto are required to decrypt notes in this browser.`,
			},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			rec := httptest.NewRecorder()

			views.View(rec, req, tt.template, tt.model)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
			}
			if got := rec.Header().Get("Cache-Control"); got != "no-store" {
				t.Fatalf("Cache-Control = %q, want no-store", got)
			}
			body := rec.Body.String()
			for _, want := range tt.want {
				if !strings.Contains(body, want) {
					t.Fatalf("body does not contain %q:\n%s", want, body)
				}
			}
			if strings.Contains(body, `"/static/site.css"`) {
				t.Fatal("body contains unversioned static asset path")
			}
		})
	}
}

func TestViewsRenderGitHubSourceLinkWhenConfigured(t *testing.T) {
	views, err := NewViews(os.DirFS("web/html"), "assets123", false, Brand{})
	if err != nil {
		t.Fatalf("NewViews returned error: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	model := CreateViewModel{
		Brand:       Brand{DisplayName: "Test Relay", GitHubURL: defaultGitHubURL},
		MaxNoteSize: 1048576,
	}

	views.View(rec, req, "create", &model)

	body := rec.Body.String()
	for _, want := range []string{
		`class="source-link"`,
		`href="https://github.com/stkom/one-time-note"`,
		`rel="noopener noreferrer"`,
		`referrerpolicy="no-referrer"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("body does not contain %q:\n%s", want, body)
		}
	}
}

func TestViewsOmitGitHubSourceLinkWhenHidden(t *testing.T) {
	views, err := NewViews(os.DirFS("web/html"), "assets123", false, Brand{})
	if err != nil {
		t.Fatalf("NewViews returned error: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	model := CreateViewModel{
		Brand:       Brand{DisplayName: "Test Relay"},
		MaxNoteSize: 1048576,
	}

	views.View(rec, req, "create", &model)

	if body := rec.Body.String(); strings.Contains(body, `class="source-link"`) {
		t.Fatalf("body contains hidden source link:\n%s", body)
	}
}

func TestViewsRenderLegalLinksWhenConfigured(t *testing.T) {
	views, err := NewViews(os.DirFS("web/html"), "assets123", false, Brand{})
	if err != nil {
		t.Fatalf("NewViews returned error: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	model := CreateViewModel{
		Brand: Brand{
			DisplayName: "Test Relay",
			LegalLinks: []LegalLink{
				{Label: "Privacy Policy", URL: "http://notes.example.test/privacy"},
				{Label: "Terms of Use", URL: "/terms"},
				{Label: "Legal notice", URL: "https://notes.example.test/legal"},
			},
		},
		MaxNoteSize: 1048576,
	}

	views.View(rec, req, "create", &model)

	body := rec.Body.String()
	for _, want := range []string{
		`<nav class="legal-links" aria-label="Legal links">`,
		`href="http://notes.example.test/privacy"`,
		`>Privacy Policy</a>`,
		`href="/terms"`,
		`>Terms of Use</a>`,
		`href="https://notes.example.test/legal"`,
		`>Legal notice</a>`,
		`rel="noopener noreferrer"`,
		`referrerpolicy="no-referrer"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("body does not contain %q:\n%s", want, body)
		}
	}
}

func TestNewViewsParsesRequiredTemplatesAtStartup(t *testing.T) {
	views, err := NewViews(os.DirFS("web/html"), "assets123", false, Brand{})
	if err != nil {
		t.Fatalf("NewViews returned error: %v", err)
	}
	for _, name := range []string{"create", "error", "note"} {
		if views.Templates[name] == nil {
			t.Fatalf("template %q was not parsed", name)
		}
	}
}

func TestNewViewsFailsWhenRequiredTemplateIsMissing(t *testing.T) {
	fsys := fstest.MapFS{
		"layout.gohtml": {Data: []byte(`{{template "body" .}}`)},
		"create.gohtml": {Data: []byte(`{{define "body"}}create{{end}}`)},
		"error.gohtml":  {Data: []byte(`{{define "body"}}error{{end}}`)},
	}

	if _, err := NewViews(fsys, "assets123", false, Brand{}); err == nil {
		t.Fatal("NewViews returned nil error for missing note template")
	}
}

func TestNewViewsFailsWhenRequiredTemplateIsMalformed(t *testing.T) {
	fsys := fstest.MapFS{
		"layout.gohtml": {Data: []byte(`{{template "body" .}}`)},
		"create.gohtml": {Data: []byte(`{{define "body"}}create{{end}}`)},
		"error.gohtml":  {Data: []byte(`{{define "body"}}error{{end}}`)},
		"note.gohtml":   {Data: []byte(`{{define "body"}}`)},
	}

	if _, err := NewViews(fsys, "assets123", false, Brand{}); err == nil {
		t.Fatal("NewViews returned nil error for malformed note template")
	}
}

func TestNewViewsReloadModeDefersParsing(t *testing.T) {
	views, err := NewViews(fstest.MapFS{}, "assets123", true, Brand{})
	if err != nil {
		t.Fatalf("NewViews returned error in reload mode: %v", err)
	}
	if views.Templates != nil {
		t.Fatalf("Templates = %#v, want nil in reload mode", views.Templates)
	}
}
