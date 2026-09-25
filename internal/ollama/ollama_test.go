package ollama_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nikolacuricdev/simple-AI-orchestration-service/internal/ollama"
)

func TestChatSendsTheConversationAndDecodesToolCalls(t *testing.T) {
	var got map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Errorf("path = %q, want /api/chat", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","tool_calls":[
			{"function":{"name":"get_weather","arguments":{"city":"Paris"}}}]}}`))
	}))
	defer server.Close()

	client := ollama.NewClient(server.URL, "test-model", 5*time.Second)
	reply, err := client.Chat(context.Background(),
		[]ollama.Message{{Role: ollama.RoleUser, Content: "weather in Paris?"}},
		[]ollama.Tool{{Type: "function", Function: ollama.ToolFunction{Name: "get_weather"}}},
	)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if got["model"] != "test-model" {
		t.Errorf("model = %v", got["model"])
	}
	// Streaming off is what makes a tool call arrive as one complete message.
	if got["stream"] != false {
		t.Errorf("stream = %v, want false", got["stream"])
	}
	if len(reply.ToolCalls) != 1 {
		t.Fatalf("tool calls = %+v", reply.ToolCalls)
	}
	call := reply.ToolCalls[0].Function
	if call.Name != "get_weather" || call.Arguments["city"] != "Paris" {
		t.Errorf("tool call = %+v", call)
	}
}

func TestChatReportsServerErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"model not found"}`, http.StatusNotFound)
	}))
	defer server.Close()

	_, err := ollama.NewClient(server.URL, "missing", 5*time.Second).
		Chat(context.Background(), nil, nil)
	if err == nil || !strings.Contains(err.Error(), "model not found") {
		t.Fatalf("err = %v, want the server's message", err)
	}
}

func TestPing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"models":[]}`))
	}))
	defer server.Close()

	if err := ollama.NewClient(server.URL, "m", time.Second).Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}

	server.Close()
	if err := ollama.NewClient(server.URL, "m", time.Second).Ping(context.Background()); err == nil {
		t.Fatal("Ping should fail against a closed server")
	}
}
