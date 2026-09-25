// Package api is the HTTP surface of the agent service. It does nothing but
// translate requests into an agent.Run and the result into JSON — keeping the
// transport out of the agent loop is what makes the loop testable.
package api

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/nikolacuricdev/simple-AI-orchestration-service/internal/agent"
)

// maxRequestBytes caps the prompt size so a client cannot stream us out of memory.
const maxRequestBytes = 64 << 10 // 64 KiB

type chatRequest struct {
	Prompt string `json:"prompt"`
}

type errorResponse struct {
	Error string `json:"error"`
}

// NewHandler wires the routes.
//
// The method-and-path patterns are Go 1.22+ ServeMux syntax, so a GET to /chat
// is routed separately from a POST and an unknown path 404s instead of falling
// through to a catch-all handler.
func NewHandler(a *agent.Agent, log *slog.Logger) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok\n")
	})

	// POST /chat is the real entrypoint; GET /chat?prompt=... exists so the
	// demo can be driven from a browser address bar.
	mux.Handle("POST /chat", chatHandler(a, log, promptFromBody))
	mux.Handle("GET /chat", chatHandler(a, log, promptFromQuery))

	return mux
}

type promptReader func(*http.Request) (string, error)

func promptFromQuery(r *http.Request) (string, error) {
	return r.URL.Query().Get("prompt"), nil
}

func promptFromBody(r *http.Request) (string, error) {
	var body chatRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, maxRequestBytes)).Decode(&body); err != nil {
		return "", err
	}
	return body.Prompt, nil
}

func chatHandler(a *agent.Agent, log *slog.Logger, readPrompt promptReader) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		prompt, err := readPrompt(r)
		if err != nil {
			writeJSON(w, log, http.StatusBadRequest, errorResponse{Error: "invalid request body"})
			return
		}
		if prompt == "" {
			writeJSON(w, log, http.StatusBadRequest, errorResponse{Error: "missing prompt"})
			return
		}

		log.InfoContext(r.Context(), "prompt received", "prompt", prompt)

		// r.Context() is cancelled when the client disconnects, which unwinds
		// the whole agent loop — including the in-flight model call.
		result, err := a.Run(r.Context(), prompt)
		if err != nil {
			status, message := classify(err)
			// The detailed error goes to the log; the client gets a stable
			// message, so internal hostnames and errors are not leaked.
			log.ErrorContext(r.Context(), "agent run failed", "error", err)
			writeJSON(w, log, status, errorResponse{Error: message})
			return
		}

		writeJSON(w, log, http.StatusOK, result)
	})
}

// classify maps an agent failure onto a status code and a safe message.
func classify(err error) (int, string) {
	switch {
	case errors.Is(err, agent.ErrStepLimit):
		return http.StatusGatewayTimeout, "the model did not reach an answer within the step limit"
	default:
		return http.StatusBadGateway, "the agent could not complete the request"
	}
}

func writeJSON(w http.ResponseWriter, log *slog.Logger, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		// The status line is already sent, so this can only be logged.
		log.Error("write response", "error", err)
	}
}
