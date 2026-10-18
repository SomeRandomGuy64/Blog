package main

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"fazon.nexus/cinema/internal/feedback"
)

// maxFeedbackBody bounds a POST /v1/feedback body (1000 characters of text is at most 4 KB of UTF-8,
// more once JSON-escaped).
const maxFeedbackBody = 32 << 10

type feedbackIn struct {
	Name    string `json:"name"`
	Text    string `json:"text"`
	Website string `json:"website"` // honeypot: hidden in the form, so only bots fill it
}

type feedbackReply struct {
	feedback.Status
	Item  *feedback.Item `json:"item,omitempty"`
	Error string         `json:"error,omitempty"`
}

func (s *server) feedbackPreflight(w http.ResponseWriter, r *http.Request) {
	s.cors(w, r)
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Access-Control-Max-Age", "86400")
	w.WriteHeader(http.StatusNoContent)
}

func writeFeedback(w http.ResponseWriter, code int, rep feedbackReply) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(rep)
}

func (s *server) feedbackList(w http.ResponseWriter, r *http.Request) {
	s.cors(w, r)
	writeFeedback(w, http.StatusOK, feedbackReply{Status: s.feedback.Status()})
}

func (s *server) feedbackPost(w http.ResponseWriter, r *http.Request) {
	s.cors(w, r)
	var in feedbackIn
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxFeedbackBody)).Decode(&in); err != nil {
		writeFeedback(w, http.StatusBadRequest, feedbackReply{Status: s.feedback.Status(), Error: "couldn't read the feedback (is it over 1000 characters?)"})
		return
	}
	if in.Website != "" {
		writeFeedback(w, http.StatusBadRequest, feedbackReply{Status: s.feedback.Status(), Error: "rejected"})
		return
	}
	it, st, err := s.feedback.Add(in.Name, in.Text)
	switch {
	case err == nil:
		slog.Info("feedback", "id", it.ID, "chars", len([]rune(it.Text)), "used", st.Used)
		writeFeedback(w, http.StatusCreated, feedbackReply{Status: st, Item: &it})
	case errors.Is(err, feedback.ErrQuota):
		writeFeedback(w, http.StatusTooManyRequests, feedbackReply{Status: st, Error: err.Error()})
	case errors.Is(err, feedback.ErrEmpty), errors.Is(err, feedback.ErrTooLong), errors.Is(err, feedback.ErrName):
		writeFeedback(w, http.StatusBadRequest, feedbackReply{Status: st, Error: err.Error()})
	default:
		slog.Error("feedback store", "err", err)
		writeFeedback(w, http.StatusInternalServerError, feedbackReply{Status: st, Error: "couldn't save the feedback"})
	}
}

// feedbackDelete removes a comment; it needs the ingest token.
func (s *server) feedbackDelete(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(w, r) {
		return
	}
	ok, err := s.feedback.Delete(r.PathValue("id"))
	switch {
	case err != nil:
		slog.Error("feedback delete", "err", err)
		http.Error(w, "store error", http.StatusInternalServerError)
	case !ok:
		http.Error(w, "no such feedback", http.StatusNotFound)
	default:
		slog.Info("feedback deleted", "id", r.PathValue("id"))
		w.WriteHeader(http.StatusNoContent)
	}
}
