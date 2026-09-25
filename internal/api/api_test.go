package api_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nikolacuricdev/simple-AI-orchestration-service/internal/agent"
	"github.com/nikolacuricdev/simple-AI-orchestration-service/internal/api"
	"github.com/nikolacuricdev/simple-AI-orchestration-service/internal/ollama"
)

type scriptedModel struct {
	reply ollama.Message
	err   error
}

func (m scriptedModel) Chat(context.Context, []ollama.Message, []ollama.Tool) (ollama.Message, error) {
	return m.reply, m.err
}

type noTools struct{}

func (noTools) List(context.Context) ([]ollama.Tool, error) { return nil, nil }
func (noTools) Call(context.Context, string, map[string]any) (string, error) {
	return "", nil
}

func newServer(t *testing.T, model agent.Model) http.Handler {
	t.Helper()
	discard := slog.New(slog.NewTextHandler(io.Discard, nil))
	return api.NewHandler(agent.New(model, noTools{}, agent.WithLogger(discard)), discard)
}

func TestChatReturnsTheAgentResult(t *testing.T) {
	handler := newServer(t, scriptedModel{reply: ollama.Message{Role: ollama.RoleAssistant, Content: "sunny"}})

	req := httptest.NewRequest(http.MethodPost, "/chat", strings.NewReader(`{"prompt":"weather?"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var got agent.Result
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Answer != "sunny" {
		t.Errorf("answer = %q", got.Answer)
	}
}

func TestChatRejectsAMissingPrompt(t *testing.T) {
	handler := newServer(t, scriptedModel{})

	for name, req := range map[string]*http.Request{
		"empty body field": httptest.NewRequest(http.MethodPost, "/chat", strings.NewReader(`{"prompt":""}`)),
		"malformed json":   httptest.NewRequest(http.MethodPost, "/chat", strings.NewReader(`{`)),
		"empty query":      httptest.NewRequest(http.MethodGet, "/chat", nil),
	} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", name, rec.Code)
		}
	}
}

func TestChatDoesNotLeakInternalErrors(t *testing.T) {
	handler := newServer(t, scriptedModel{err: errInternal{}})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/chat?prompt=hi", nil))

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "internal-hostname") {
		t.Errorf("internal error leaked to the client: %s", rec.Body)
	}
}

func TestRoutingIsExplicit(t *testing.T) {
	handler := newServer(t, scriptedModel{})

	cases := map[*http.Request]int{
		httptest.NewRequest(http.MethodGet, "/healthz", nil):     http.StatusOK,
		httptest.NewRequest(http.MethodGet, "/", nil):            http.StatusNotFound,
		httptest.NewRequest(http.MethodDelete, "/chat", nil):     http.StatusMethodNotAllowed,
		httptest.NewRequest(http.MethodGet, "/favicon.ico", nil): http.StatusNotFound,
	}
	for req, want := range cases {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Errorf("%s %s: status = %d, want %d", req.Method, req.URL.Path, rec.Code, want)
		}
	}
}

type errInternal struct{}

func (errInternal) Error() string { return "dial tcp internal-hostname:11434: connection refused" }
