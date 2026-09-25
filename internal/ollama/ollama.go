// Package ollama is a small client for the subset of the Ollama HTTP API this
// project needs: a single blocking /api/chat call with tool support.
//
// Ollama speaks the same message/tool-call shape as most chat completion APIs,
// so this file doubles as a map of the four concepts every LLM integration has:
// a Message, a ToolCall the model emits, a Tool you advertise, and the reply.
//
// API reference: https://github.com/ollama/ollama/blob/main/docs/api.md
package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Message roles, as defined by the Ollama chat API.
const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
)

// Message is one entry in the conversation sent to the model.
//
// The whole conversation is resent on every call: the model is stateless, so
// the transcript the agent builds up *is* the memory.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content,omitempty"`

	// ToolCalls is set by the model on an assistant message when it wants a
	// tool run before it can answer.
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`

	// ToolName is set by us on a RoleTool message to say which tool produced
	// Content. Without it the model cannot match results to calls when it
	// requested more than one tool in a turn.
	ToolName string `json:"tool_name,omitempty"`
}

// ToolCall is the model's request to run one tool.
type ToolCall struct {
	Function FunctionCall `json:"function"`
}

// FunctionCall carries the tool name and the arguments the model invented for
// it. Arguments are only as trustworthy as the model: validate before use.
type FunctionCall struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

// Tool describes a callable to the model. This is the only thing the model
// knows about your tools, so Description and Parameters are prompt engineering:
// vague ones produce wrong calls.
type Tool struct {
	Type     string       `json:"type"` // always "function"
	Function ToolFunction `json:"function"`
}

// ToolFunction is the body of a Tool description.
type ToolFunction struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"` // JSON Schema describing the arguments
}

type chatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Tools    []Tool    `json:"tools,omitempty"`
	Stream   bool      `json:"stream"`
}

type chatResponse struct {
	Message Message `json:"message"`
}

// Client talks to one Ollama server about one model.
type Client struct {
	baseURL string
	model   string
	http    *http.Client
}

// NewClient returns a client for the Ollama server at baseURL.
//
// The timeout is deliberately generous: a cold local model can take tens of
// seconds for its first token. It must never be zero, though — the default
// http.Client has no timeout at all, and a wedged model would pin the
// goroutine forever.
func NewClient(baseURL, model string, timeout time.Duration) *Client {
	return &Client{
		baseURL: baseURL,
		model:   model,
		http:    &http.Client{Timeout: timeout},
	}
}

// Chat sends the conversation and the available tools to the model and returns
// its next message, which is either a final answer or one or more tool calls.
func (c *Client) Chat(ctx context.Context, messages []Message, tools []Tool) (Message, error) {
	body, err := json.Marshal(chatRequest{
		Model:    c.model,
		Messages: messages,
		Tools:    tools,
		// Streaming would be nicer for a UI, but it makes the agent loop
		// harder to read, and tool calls only become actionable once the
		// message is complete anyway.
		Stream: false,
	})
	if err != nil {
		return Message{}, fmt.Errorf("encode chat request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return Message{}, fmt.Errorf("build chat request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return Message{}, fmt.Errorf("call ollama: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return Message{}, fmt.Errorf("read ollama response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return Message{}, fmt.Errorf("ollama returned %s: %s", resp.Status, respBody)
	}

	var parsed chatResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return Message{}, fmt.Errorf("decode ollama response: %w", err)
	}
	return parsed.Message, nil
}

// Ping reports whether the Ollama server is up. Used at startup to wait for
// the container to finish booting and pulling the model.
func (c *Client) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/tags", nil)
	if err != nil {
		return err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseBytes))

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ollama returned %s", resp.Status)
	}
	return nil
}

// maxResponseBytes caps what we read from the model server so a runaway
// response cannot exhaust memory.
const maxResponseBytes = 8 << 20 // 8 MiB
