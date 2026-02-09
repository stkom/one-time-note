package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"
	"time"
)

func TestNoteJSONAPICreateOpenAndSecondOpenFails(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	ticket, err := app.NoteService.CreateTicket()
	if err != nil {
		t.Fatalf("CreateTicket returned error: %v", err)
	}
	createBody := createNoteRequest{
		Ticket:           ticket.Value,
		BurnToken:        &ticket.BurnToken,
		Payload:          testPayload("api secret"),
		ExpiresInSeconds: int((2 * time.Hour) / time.Second),
	}
	createRec := performJSONRequest(t, app, http.MethodPost, "/api/notes/"+ticket.Data.ID.String(), createBody)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body = %s", createRec.Code, createRec.Body.String())
	}
	assertNoStoreNoCookie(t, createRec)

	var createResp createNoteResponse
	if err := json.Unmarshal(createRec.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("create response was not JSON: %v", err)
	}
	if createResp.ID != ticket.Data.ID {
		t.Fatalf("created id = %q, want %q", createResp.ID, ticket.Data.ID)
	}
	if createResp.ExpiresInSeconds != int((2*time.Hour)/time.Second) {
		t.Fatalf("expiresInSeconds = %d, want %d", createResp.ExpiresInSeconds, int((2*time.Hour)/time.Second))
	}

	openBody := openNoteRequest{BurnToken: &ticket.BurnToken}
	openRec := performJSONRequest(t, app, http.MethodPost, "/api/notes/"+ticket.Data.ID.String()+"/open", openBody)
	if openRec.Code != http.StatusOK {
		t.Fatalf("open status = %d body = %s", openRec.Code, openRec.Body.String())
	}
	assertNoStoreNoCookie(t, openRec)
	var openResp openNoteResponse
	if err := json.Unmarshal(openRec.Body.Bytes(), &openResp); err != nil {
		t.Fatalf("open response was not JSON: %v", err)
	}
	if openResp.Payload != testPayload("api secret") {
		t.Fatalf("open payload = %#v, want %#v", openResp.Payload, testPayload("api secret"))
	}

	secondOpen := performJSONRequest(t, app, http.MethodPost, "/api/notes/"+ticket.Data.ID.String()+"/open", openBody)
	if secondOpen.Code != http.StatusNotFound {
		t.Fatalf("second open status = %d body = %s", secondOpen.Code, secondOpen.Body.String())
	}
	if got := jsonErrorCode(t, secondOpen); got != "note_open_failed" {
		t.Fatalf("second open error = %q, want note_open_failed", got)
	}
}

func TestTicketJSONAPIReturnsUsableTicket(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	rec := performJSONRequest(t, app, http.MethodPost, "/api/tickets", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("ticket status = %d body = %s", rec.Code, rec.Body.String())
	}
	assertNoStoreNoCookie(t, rec)

	var ticketResp createTicketResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &ticketResp); err != nil {
		t.Fatalf("ticket response was not JSON: %v", err)
	}
	if ticketResp.ID == (NoteID{}) || ticketResp.Ticket == "" || ticketResp.BurnToken == (BurnToken{}) || ticketResp.TicketExpiresAt.IsZero() {
		t.Fatalf("ticket response missing required fields: %#v", ticketResp)
	}

	createRec := performJSONRequest(t, app, http.MethodPost, "/api/notes/"+ticketResp.ID.String(), createNoteRequest{
		Ticket:           ticketResp.Ticket,
		BurnToken:        &ticketResp.BurnToken,
		Payload:          testPayload("api secret"),
		ExpiresInSeconds: int((24 * time.Hour) / time.Second),
	})
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body = %s", createRec.Code, createRec.Body.String())
	}
}

func TestConfigJSONAPIReturnsPublicLimits(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	rec := performRequest(app, http.MethodGet, "/api/config", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s, want %d", rec.Code, rec.Body.String(), http.StatusOK)
	}
	assertNoStoreNoCookie(t, rec)
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}

	var response map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("response was not JSON: %v\n%s", err, rec.Body.String())
	}
	want := map[string]any{
		"note": map[string]any{
			"maxPayloadBytes":      float64(defaultMaxNoteSize),
			"expirationMinSeconds": float64(minNoteLifetime / time.Second),
			"expirationMaxSeconds": float64(maxNoteLifetime / time.Second),
		},
	}
	if !reflect.DeepEqual(response, want) {
		t.Fatalf("config response = %#v, want %#v", response, want)
	}
}

func TestCreateNoteRejectsEmptyPayload(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	ticket, err := app.NoteService.CreateTicket()
	if err != nil {
		t.Fatalf("CreateTicket returned error: %v", err)
	}
	rec := performJSONRequest(t, app, http.MethodPost, "/api/notes/"+ticket.Data.ID.String(), createNoteRequest{
		Ticket:           ticket.Value,
		BurnToken:        &ticket.BurnToken,
		Payload:          "",
		ExpiresInSeconds: int((24 * time.Hour) / time.Second),
	})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %s, want %d", rec.Code, rec.Body.String(), http.StatusBadRequest)
	}
	if got := jsonErrorCode(t, rec); got != "invalid_request" {
		t.Fatalf("error = %q, want invalid_request", got)
	}
}

func TestCreateNoteRejectsInvalidExpiration(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	tests := []struct {
		name string
		body func(Ticket) any
	}{
		{
			name: "omitted",
			body: func(ticket Ticket) any {
				return map[string]any{
					"ticket":    ticket.Value,
					"burnToken": ticket.BurnToken,
					"payload":   testPayload("api secret"),
				}
			},
		},
		{
			name: "zero",
			body: func(ticket Ticket) any {
				return createNoteRequest{
					Ticket:           ticket.Value,
					BurnToken:        &ticket.BurnToken,
					Payload:          testPayload("api secret"),
					ExpiresInSeconds: 0,
				}
			},
		},
		{
			name: "negative",
			body: func(ticket Ticket) any {
				return createNoteRequest{
					Ticket:           ticket.Value,
					BurnToken:        &ticket.BurnToken,
					Payload:          testPayload("api secret"),
					ExpiresInSeconds: -1,
				}
			},
		},
		{
			name: "too small",
			body: func(ticket Ticket) any {
				return createNoteRequest{
					Ticket:           ticket.Value,
					BurnToken:        &ticket.BurnToken,
					Payload:          testPayload("api secret"),
					ExpiresInSeconds: int((30 * time.Minute) / time.Second),
				}
			},
		},
		{
			name: "too large",
			body: func(ticket Ticket) any {
				return createNoteRequest{
					Ticket:           ticket.Value,
					BurnToken:        &ticket.BurnToken,
					Payload:          testPayload("api secret"),
					ExpiresInSeconds: int((maxNoteLifetime + time.Second) / time.Second),
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ticket, err := app.NoteService.CreateTicket()
			if err != nil {
				t.Fatalf("CreateTicket returned error: %v", err)
			}
			rec := performJSONRequest(t, app, http.MethodPost, "/api/notes/"+ticket.Data.ID.String(), tt.body(ticket))

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body = %s, want %d", rec.Code, rec.Body.String(), http.StatusBadRequest)
			}
			if got := jsonErrorCode(t, rec); got != "invalid_expiration" {
				t.Fatalf("error = %q, want invalid_expiration", got)
			}
		})
	}
}

func TestCreateNoteAcceptsNonHourExpirationWithinRange(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	ticket, err := app.NoteService.CreateTicket()
	if err != nil {
		t.Fatalf("CreateTicket returned error: %v", err)
	}
	const expiresInSeconds = 90*60 + 1
	rec := performJSONRequest(t, app, http.MethodPost, "/api/notes/"+ticket.Data.ID.String(), createNoteRequest{
		Ticket:           ticket.Value,
		BurnToken:        &ticket.BurnToken,
		Payload:          testPayload("api secret"),
		ExpiresInSeconds: expiresInSeconds,
	})

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body = %s, want %d", rec.Code, rec.Body.String(), http.StatusCreated)
	}
	var response createNoteResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("response was not JSON: %v\n%s", err, rec.Body.String())
	}
	if response.ExpiresInSeconds != expiresInSeconds {
		t.Fatalf("expiresInSeconds = %d, want %d", response.ExpiresInSeconds, expiresInSeconds)
	}
}

func TestCreateNoteRejectsWrongContentTypeAndUnknownFields(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	id := NewNoteID()
	req := httptest.NewRequest(http.MethodPost, "/api/notes/"+id.String(), bytes.NewBufferString("{}"))
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()

	newServerHandler(app).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnsupportedMediaType)
	}
	if got := jsonErrorCode(t, rec); got != "invalid_request" {
		t.Fatalf("error = %q, want invalid_request", got)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/notes/"+id.String(), bytes.NewBufferString(`{"unknown":true}`))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()

	newServerHandler(app).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if got := jsonErrorCode(t, rec); got != "invalid_request" {
		t.Fatalf("error = %q, want invalid_request", got)
	}
}

func TestCreateNoteLetsServiceValidateTicket(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	id := NewNoteID()
	rec := performJSONRequest(t, app, http.MethodPost, "/api/notes/"+id.String(), createNoteRequest{
		BurnToken:        new(NewBurnToken()),
		Payload:          testPayload("secret"),
		ExpiresInSeconds: int((24 * time.Hour) / time.Second),
	})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %s, want %d", rec.Code, rec.Body.String(), http.StatusBadRequest)
	}
	if got := jsonErrorCode(t, rec); got != "ticket_unusable" {
		t.Fatalf("error = %q, want ticket_unusable", got)
	}
}

func TestLegacyNotePostNoLongerCreatesNotes(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	ticket, err := app.NoteService.CreateTicket()
	if err != nil {
		t.Fatalf("CreateTicket returned error: %v", err)
	}
	rec := performJSONRequest(t, app, http.MethodPost, "/note/"+ticket.Data.ID.String(), createNoteRequest{
		Ticket:    ticket.Value,
		BurnToken: &ticket.BurnToken,
		Payload:   testPayload("secret"),
	})

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d body = %s, want %d", rec.Code, rec.Body.String(), http.StatusNotFound)
	}
}

func TestPreviewDoesNotRevealNoteExistence(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	id := NewNoteID()
	req := httptest.NewRequest(http.MethodGet, "/note/"+id.String(), nil)
	rec := httptest.NewRecorder()

	newServerHandler(app).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("Opening this note will destroy it on the server.")) {
		t.Fatalf("preview body did not render generic open confirmation:\n%s", rec.Body.String())
	}
	assertNoStoreNoCookie(t, rec)
}

func TestRoutesDoNotSetCookies(t *testing.T) {
	app, cleanup := setupTestApp(t)
	defer cleanup()

	id := NewNoteID()
	for _, path := range []string{"/", "/note/" + id.String(), "/healthz"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rec := httptest.NewRecorder()

			newServerHandler(app).ServeHTTP(rec, req)

			if cookies := rec.Header().Values("Set-Cookie"); len(cookies) != 0 {
				t.Fatalf("Set-Cookie headers = %v, want none", cookies)
			}
		})
	}
}

func setupTestApp(t *testing.T) (*app, func()) {
	t.Helper()
	service, db, cleanup := setupTestService(t, 1024*1024)
	cfg, err := NewConfigWithOptions(envMap(map[string]string{}), StartupOptions{Development: true})
	if err != nil {
		cleanup()
		t.Fatalf("NewConfigWithOptions returned error: %v", err)
	}
	views, err := NewViews(os.DirFS("web/html"), "test-assets", false, cfg.Brand)
	if err != nil {
		cleanup()
		t.Fatalf("NewViews returned error: %v", err)
	}
	return &app{
		AssetVersion: "test-assets",
		Cfg:          cfg,
		DB:           db,
		NoteService:  service,
		RateLimiter:  NewRateLimiter(cfg.RateLimit),
		StaticFS:     os.DirFS("web/static"),
		Views:        views,
	}, cleanup
}

func performJSONRequest(t *testing.T, app *app, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("json.Marshal returned error: %v", err)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Accept", "application/json")
	rec := httptest.NewRecorder()

	newServerHandler(app).ServeHTTP(rec, req)
	return rec
}

func assertNoStoreNoCookie(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	if cookies := rec.Header().Values("Set-Cookie"); len(cookies) != 0 {
		t.Fatalf("Set-Cookie headers = %v, want none", cookies)
	}
}

func jsonErrorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var response errorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("response was not JSON error: %v\n%s", err, rec.Body.String())
	}
	return response.Error
}
