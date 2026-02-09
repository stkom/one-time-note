package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"
)

const (
	jsonBodyOverhead          = 256
	statusInsufficientStorage = 507
)

var errRequestTooLarge = errors.New("request too large")

type createNoteRequest struct {
	Ticket           string     `json:"ticket"`
	BurnToken        *BurnToken `json:"burnToken"`
	Payload          string     `json:"payload"`
	ExpiresInSeconds int        `json:"expiresInSeconds"`
}

type configResponse struct {
	Note configNoteResponse `json:"note"`
}

type configNoteResponse struct {
	MaxPayloadBytes      int `json:"maxPayloadBytes"`
	ExpirationMinSeconds int `json:"expirationMinSeconds"`
	ExpirationMaxSeconds int `json:"expirationMaxSeconds"`
}

type createTicketResponse struct {
	ID              NoteID    `json:"id"`
	Ticket          string    `json:"ticket"`
	BurnToken       BurnToken `json:"burnToken"`
	TicketExpiresAt time.Time `json:"ticketExpiresAt"`
}

type createNoteResponse struct {
	ID               NoteID `json:"id"`
	ExpiresInSeconds int    `json:"expiresInSeconds"`
}

type openNoteRequest struct {
	BurnToken *BurnToken `json:"burnToken"`
}

type openNoteResponse struct {
	Payload string `json:"payload"`
}

func HandleCreateNoteView(views *Views, cfg *Config) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		views.View(w, r, "create", createViewModel(cfg, GetHTTPContext(r).PublicOrigin))
	})
}

func createViewModel(cfg *Config, publicOrigin string) CreateViewModel {
	return CreateViewModel{
		Brand:        cfg.Brand,
		MaxNoteSize:  cfg.MaxNoteSize,
		PublicOrigin: publicOrigin,
	}
}

func HandleGetConfig(cfg *Config) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, configResponse{
			Note: configNoteResponse{
				MaxPayloadBytes:      cfg.MaxNoteSize,
				ExpirationMinSeconds: int(minNoteLifetime / time.Second),
				ExpirationMaxSeconds: int(maxNoteLifetime / time.Second),
			},
		})
	})
}

func HandleCreateTicket(service *NoteService) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ticket, err := service.CreateTicket()
		if err != nil {
			writeInternalJSONError(w, r, "ticket_create_failed", err)
			return
		}

		writeJSON(w, http.StatusOK, createTicketResponse{
			ID:              ticket.Data.ID,
			Ticket:          ticket.Value,
			BurnToken:       ticket.BurnToken,
			TicketExpiresAt: ticket.Data.Exp,
		})
	})
}

func HandleCreateNote(service *NoteService) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := requireJSONContentType(r); err != nil {
			writeJSON(w, http.StatusUnsupportedMediaType, errorResponse{Error: "invalid_request"})
			return
		}

		id, err := ParseNoteID(r.PathValue("id"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid_request"})
			return
		}

		var req createNoteRequest
		if err := decodeStrictJSON(w, r, maxCreateNoteBodySize(service.maxSize), &req); err != nil {
			status := http.StatusBadRequest
			if errors.Is(err, errRequestTooLarge) {
				status = http.StatusRequestEntityTooLarge
			}
			writeJSON(w, status, errorResponse{Error: "invalid_request"})
			return
		}
		if req.BurnToken == nil {
			writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid_request"})
			return
		}
		lifetime, err := requestedLifetime(req.ExpiresInSeconds)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid_expiration"})
			return
		}
		savedID, err := service.CreateNote(id, req.Ticket, *req.BurnToken, []byte(req.Payload), lifetime)
		if err != nil {
			switch {
			case errors.Is(err, ErrPayloadTooLarge):
				writeJSON(w, http.StatusRequestEntityTooLarge, errorResponse{Error: "note_too_large"})
			case errors.Is(err, ErrStorageFull):
				writeJSON(w, statusInsufficientStorage, errorResponse{Error: "storage_full"})
			case errors.Is(err, ErrLifetimeInvalid):
				writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid_expiration"})
			case errors.Is(err, ErrTicketInvalid), errors.Is(err, ErrTicketExpired), errors.Is(err, ErrTicketUnusable):
				writeJSON(w, ticketErrorStatus(err), errorResponse{Error: "ticket_unusable"})
				logSecurityEvent("invalid_ticket", "route", routePattern(r), "reason", ticketErrorReason(err), "client", GetHTTPContext(r).ClientKey, "status", ticketErrorStatus(err))
			case errors.Is(err, ErrPayloadEmpty):
				writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid_request"})
			default:
				writeInternalJSONError(w, r, "note_save_failed", err)
			}
			return
		}

		writeJSON(w, http.StatusCreated, createNoteResponse{
			ID:               savedID,
			ExpiresInSeconds: int(lifetime / time.Second),
		})
	})
}

func HandleOpenNote(service *NoteService) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := requireJSONContentType(r); err != nil {
			writeJSON(w, http.StatusUnsupportedMediaType, errorResponse{Error: "invalid_request"})
			return
		}

		id, err := ParseNoteID(r.PathValue("id"))
		if err != nil {
			writeJSON(w, http.StatusNotFound, errorResponse{Error: "note_open_failed"})
			return
		}

		var req openNoteRequest
		if err := decodeStrictJSON(w, r, maxOpenNoteBodySize(), &req); err != nil {
			if errors.Is(err, errInvalidBurnToken) {
				recordFailedOpen(w, r, "invalid_token")
				return
			}
			writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid_request"})
			return
		}
		if req.BurnToken == nil {
			recordFailedOpen(w, r, "invalid_token")
			return
		}

		note, err := service.OpenNote(id, *req.BurnToken)
		if err != nil {
			if errors.Is(err, ErrNoteOpenFailed) {
				recordFailedOpen(w, r, "open_failed")
				return
			}
			writeInternalJSONError(w, r, "note_open_failed_internal", err)
			return
		}

		writeJSON(w, http.StatusOK, openNoteResponse{Payload: string(note.Payload)})
	})
}

func HandlePreviewNote(views *Views) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idStr := r.PathValue("id")
		if _, err := ParseNoteID(idStr); err != nil {
			views.NotFound(w, r)
			return
		}

		model := NoteViewModel{Brand: views.Brand, ID: idStr}
		views.View(w, r, "note", &model)
	})
}

func decodeStrictJSON(w http.ResponseWriter, r *http.Request, limit int, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, int64(limit))
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		if isMaxBytesError(err) {
			return errRequestTooLarge
		}
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if isMaxBytesError(err) {
			return errRequestTooLarge
		}
		return errors.New("trailing JSON data")
	}
	return nil
}

func isMaxBytesError(err error) bool {
	var maxBytesError *http.MaxBytesError
	return errors.As(err, &maxBytesError)
}

func maxCreateNoteBodySize(maxPayloadBytes int) int {
	return maxPayloadBytes + maxTicketLen + len((BurnToken{}).String()) + jsonBodyOverhead
}

func maxOpenNoteBodySize() int {
	return len((BurnToken{}).String()) + jsonBodyOverhead
}

func requestedLifetime(seconds int) (time.Duration, error) {
	if seconds < int(minNoteLifetime/time.Second) || seconds > int(maxNoteLifetime/time.Second) {
		return 0, ErrLifetimeInvalid
	}
	return time.Duration(seconds) * time.Second, nil
}

func ticketErrorStatus(err error) int {
	if errors.Is(err, ErrTicketUnusable) {
		return http.StatusConflict
	}
	return http.StatusBadRequest
}

func ticketErrorReason(err error) string {
	switch {
	case errors.Is(err, ErrTicketExpired):
		return "expired"
	case errors.Is(err, ErrTicketUnusable):
		return "unusable"
	default:
		return "invalid"
	}
}

func recordFailedOpen(w http.ResponseWriter, r *http.Request, reason string) {
	status := http.StatusNotFound
	logSecurityEvent("open_failed", "route", routePattern(r), "reason", reason, "client", GetHTTPContext(r).ClientKey, "status", status)
	writeJSON(w, status, errorResponse{Error: "note_open_failed"})
}
