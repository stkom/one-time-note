package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"strings"
)

type errorResponse struct {
	Error string `json:"error"`
}

func setNoStore(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	setNoStore(w)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeErrorResponse(w http.ResponseWriter, r *http.Request, status int, code string) {
	if wantsJSON(r) {
		writeJSON(w, status, errorResponse{Error: code})
		return
	}
	setNoStore(w)
	http.Error(w, http.StatusText(status), status)
}

func writeInternalJSONError(w http.ResponseWriter, r *http.Request, event string, err error) {
	if ctx, ok := r.Context().Value(httpContextKey).(*HTTPContext); ok {
		ctx.Err = err
	}
	logRequestError(r, event, err)
	writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "internal_error"})
}

func wantsJSON(r *http.Request) bool {
	if isJSONRoute(r) {
		return true
	}
	accept := r.Header.Get("Accept")
	return strings.Contains(accept, "application/json")
}

func requireJSONContentType(r *http.Request) error {
	contentType := r.Header.Get("Content-Type")
	if contentType == "" {
		return errors.New("content type is required")
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return fmt.Errorf("invalid content type: %w", err)
	}
	if mediaType != "application/json" {
		return fmt.Errorf("unsupported content type: %s", mediaType)
	}
	return nil
}
