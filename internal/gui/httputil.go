package gui

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// contextWithTimeout is a small helper for one-shot operations.
func contextWithTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

// writeJSON serializes v as JSON with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeErr writes a JSON error envelope: {"error": "..."}.
func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// maxRequestBody bounds the size of a decoded JSON request body (1 MiB) so a
// local page cannot exhaust memory with an oversized payload.
const maxRequestBody = 1 << 20

// decodeJSON reads and decodes a JSON request body into dst, capping the body at
// maxRequestBody bytes.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}
