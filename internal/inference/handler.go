package inference

import (
	"encoding/json"
	"net/http"
)

// Handler exposes the OpenAI-compatible endpoints over HTTP.
type Handler struct{ mgr *Manager }

// NewHandler wraps a Manager.
func NewHandler(mgr *Manager) *Handler { return &Handler{mgr: mgr} }

// Register attaches /v1 routes to a mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/chat/completions", h.chatCompletions)
	mux.HandleFunc("POST /v1/completions", h.completions)
	mux.HandleFunc("GET /v1/models", h.models)
}

func (h *Handler) chatCompletions(w http.ResponseWriter, r *http.Request) {
	var req ChatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Model == "" {
		writeErr(w, http.StatusBadRequest, "model is required")
		return
	}
	resp, err := h.mgr.ChatCompletion(r.Context(), req)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// completions adapts the legacy /v1/completions to the chat path.
func (h *Handler) completions(w http.ResponseWriter, r *http.Request) {
	var raw struct {
		Model       string  `json:"model"`
		Prompt      string  `json:"prompt"`
		Temperature float64 `json:"temperature"`
		MaxTokens   int     `json:"max_tokens"`
	}
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	resp, err := h.mgr.ChatCompletion(r.Context(), ChatCompletionRequest{
		Model:       raw.Model,
		Messages:    []Message{{Role: "user", Content: raw.Prompt}},
		Temperature: raw.Temperature,
		MaxTokens:   raw.MaxTokens,
	})
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) models(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, ModelsResponse{Object: "list", Data: h.mgr.Models()})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{"message": msg, "type": "faraday_error"},
	})
}

// Manager exposes the underlying manager (used by the agent runtime in-process).
func (h *Handler) Manager() *Manager { return h.mgr }
