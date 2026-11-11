package gui

import (
	"net/http"
	"strings"
)

func (s *Server) handlePlaybackLease(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID     string `json:"id"`
		Active bool   `json:"active"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		return
	}
	if len(body.ID) < 8 || len(body.ID) > 80 || strings.Trim(body.ID, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_") != "" {
		writeErr(w, http.StatusBadRequest, "invalid player lease")
		return
	}
	s.mgr.limiter.SetPlaybackLease(body.ID, body.Active)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
