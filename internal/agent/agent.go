// Package agent implements the agent loop: the cycle of asking a model for its
// next move, running any tool it asks for, feeding the result back, and
// repeating until the model answers in prose.
//
// The loop depends on two interfaces — Model and Toolset — and on nothing else.
// That is the whole design: where the tools come from (MCP here, but it could
// be Go functions or a remote API) and which model answers are decisions made
// by the caller in cmd/agent, not by the loop.
package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/nikolacuricdev/simple-AI-orchestration-service/internal/ollama"
)

// Model is the LLM the agent reasons with.
type Model interface {
	Chat(ctx context.Context, messages []ollama.Message, tools []ollama.Tool) (ollama.Message, error)
}

// Toolset is the set of tools the agent may offer the model.
//
// Call returns the tool's output as text. A returned error is *not* fatal: the
// agent hands it to the model as the tool's result, which is what lets the
// model retry with different arguments or explain the failure to the user.
type Toolset interface {
	List(ctx context.Context) ([]ollama.Tool, error)
	Call(ctx context.Context, name string, args map[string]any) (string, error)
}

// ErrStepLimit is returned when the model kept asking for tools past MaxSteps.
// Every agent loop needs a bound like this: without one, a model stuck in a
// tool-calling rut loops until the process runs out of memory.
var ErrStepLimit = errors.New("agent: step limit reached without a final answer")

// DefaultSystemPrompt keeps the demo model on task. Small models drift without
// one; they will answer from parametric memory instead of calling the tool.
const DefaultSystemPrompt = `You are a weather assistant.
Use the get_weather tool whenever the user asks about weather in a city; never guess a forecast.
If a tool reports an error, tell the user plainly what went wrong.
Answer in one or two short sentences.`

// Agent runs the loop. It is stateless and safe for concurrent use: each Run
// builds its own transcript.
type Agent struct {
	model        Model
	tools        Toolset
	log          *slog.Logger
	systemPrompt string
	maxSteps     int
}

// Option configures an Agent.
type Option func(*Agent)

// WithSystemPrompt replaces DefaultSystemPrompt.
func WithSystemPrompt(p string) Option { return func(a *Agent) { a.systemPrompt = p } }

// WithMaxSteps sets how many model turns a single Run may take.
func WithMaxSteps(n int) Option { return func(a *Agent) { a.maxSteps = n } }

// WithLogger sets the logger used for the per-step trace.
func WithLogger(l *slog.Logger) Option { return func(a *Agent) { a.log = l } }

// New builds an Agent over the given model and toolset.
func New(model Model, tools Toolset, opts ...Option) *Agent {
	a := &Agent{
		model:        model,
		tools:        tools,
		log:          slog.Default(),
		systemPrompt: DefaultSystemPrompt,
		maxSteps:     5,
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// ToolCall records one tool invocation, so the HTTP response can show what the
// model actually did. Exposing the trace is what turns a black box into a demo.
type ToolCall struct {
	Step   int            `json:"step"`
	Name   string         `json:"name"`
	Args   map[string]any `json:"args"`
	Result string         `json:"result"`
	Failed bool           `json:"failed,omitempty"`
}

// Result is the outcome of a single Run.
type Result struct {
	Answer    string     `json:"answer"`
	Steps     int        `json:"steps"`
	ToolCalls []ToolCall `json:"tool_calls"`
}

// Run drives the agent loop to a final answer.
//
//	 ┌─────────────────────────────────────────────┐
//	 │                                             │
//	 ▼                                             │
//	ask the model ──► did it request tools? ──no──► done: return its prose
//	                          │
//	                         yes
//	                          │
//	                          ▼
//	                 run each tool, append
//	                 its output as a message ──────┘
func (a *Agent) Run(ctx context.Context, prompt string) (Result, error) {
	// The tool list is fetched per run rather than cached, so a tool added to
	// the MCP server shows up without restarting this service.
	tools, err := a.tools.List(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("list tools: %w", err)
	}

	messages := []ollama.Message{
		{Role: ollama.RoleSystem, Content: a.systemPrompt},
		{Role: ollama.RoleUser, Content: prompt},
	}

	var trace []ToolCall

	for step := 1; step <= a.maxSteps; step++ {
		reply, err := a.model.Chat(ctx, messages, tools)
		if err != nil {
			return Result{}, fmt.Errorf("model turn %d: %w", step, err)
		}

		// The assistant message goes back into the transcript either way: on a
		// tool turn it is the record of *which* calls the results answer.
		messages = append(messages, reply)

		if len(reply.ToolCalls) == 0 {
			a.log.InfoContext(ctx, "agent finished", "steps", step, "tool_calls", len(trace))
			return Result{Answer: reply.Content, Steps: step, ToolCalls: trace}, nil
		}

		for _, call := range reply.ToolCalls {
			record := a.runTool(ctx, step, call)
			trace = append(trace, record)

			messages = append(messages, ollama.Message{
				Role:     ollama.RoleTool,
				ToolName: record.Name,
				Content:  record.Result,
			})
		}
	}

	return Result{Steps: a.maxSteps, ToolCalls: trace}, ErrStepLimit
}

// runTool executes one tool call and turns any failure into text for the model.
func (a *Agent) runTool(ctx context.Context, step int, call ollama.ToolCall) ToolCall {
	name, args := call.Function.Name, call.Function.Arguments
	a.log.InfoContext(ctx, "tool call", "step", step, "tool", name, "args", args)

	output, err := a.tools.Call(ctx, name, args)
	if err != nil {
		// Deliberately not fatal. Telling the model "that failed, here is why"
		// gives it a chance to correct itself; aborting the request does not.
		a.log.WarnContext(ctx, "tool failed", "step", step, "tool", name, "error", err)
		return ToolCall{
			Step:   step,
			Name:   name,
			Args:   args,
			Result: fmt.Sprintf("Tool %q failed: %v", name, err),
			Failed: true,
		}
	}

	a.log.InfoContext(ctx, "tool result", "step", step, "tool", name, "result", output)
	return ToolCall{Step: step, Name: name, Args: args, Result: output}
}
