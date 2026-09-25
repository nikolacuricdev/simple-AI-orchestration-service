package agent_test

import (
	"context"
	"errors"
	"testing"

	"github.com/nikolacuricdev/simple-AI-orchestration-service/internal/agent"
	"github.com/nikolacuricdev/simple-AI-orchestration-service/internal/ollama"
)

// The Model and Toolset interfaces exist so the loop can be tested without a
// model or a network. These two fakes are the whole test harness.

type fakeModel struct {
	replies []ollama.Message
	seen    [][]ollama.Message // the transcript as of each turn
}

func (m *fakeModel) Chat(_ context.Context, messages []ollama.Message, _ []ollama.Tool) (ollama.Message, error) {
	m.seen = append(m.seen, append([]ollama.Message(nil), messages...))
	if len(m.replies) == 0 {
		return ollama.Message{Role: ollama.RoleAssistant, Content: "done"}, nil
	}
	next := m.replies[0]
	m.replies = m.replies[1:]
	return next, nil
}

type fakeTools struct {
	result string
	err    error
	calls  int
}

func (t *fakeTools) List(context.Context) ([]ollama.Tool, error) {
	return []ollama.Tool{{Type: "function", Function: ollama.ToolFunction{Name: "get_weather"}}}, nil
}

func (t *fakeTools) Call(_ context.Context, _ string, _ map[string]any) (string, error) {
	t.calls++
	return t.result, t.err
}

func toolCall(name string, args map[string]any) ollama.Message {
	return ollama.Message{
		Role:      ollama.RoleAssistant,
		ToolCalls: []ollama.ToolCall{{Function: ollama.FunctionCall{Name: name, Arguments: args}}},
	}
}

func TestRunAnswersWithoutTools(t *testing.T) {
	model := &fakeModel{replies: []ollama.Message{{Role: ollama.RoleAssistant, Content: "hello"}}}
	tools := &fakeTools{}

	got, err := agent.New(model, tools).Run(context.Background(), "hi")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got.Answer != "hello" {
		t.Errorf("answer = %q, want %q", got.Answer, "hello")
	}
	if got.Steps != 1 {
		t.Errorf("steps = %d, want 1", got.Steps)
	}
	if tools.calls != 0 {
		t.Errorf("tool called %d times, want 0", tools.calls)
	}
}

func TestRunFeedsToolResultBackToModel(t *testing.T) {
	model := &fakeModel{replies: []ollama.Message{
		toolCall("get_weather", map[string]any{"city": "Paris"}),
		{Role: ollama.RoleAssistant, Content: "It is 19C in Paris."},
	}}
	tools := &fakeTools{result: `{"city":"Paris","temperature_c":19}`}

	got, err := agent.New(model, tools).Run(context.Background(), "weather in Paris?")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got.Answer != "It is 19C in Paris." {
		t.Errorf("answer = %q", got.Answer)
	}
	if got.Steps != 2 {
		t.Errorf("steps = %d, want 2", got.Steps)
	}
	if len(got.ToolCalls) != 1 || got.ToolCalls[0].Name != "get_weather" {
		t.Fatalf("tool trace = %+v", got.ToolCalls)
	}

	// The second turn must show the model the tool output, tagged with the
	// tool's name — that tagging is what lets it match results to calls.
	second := model.seen[1]
	last := second[len(second)-1]
	if last.Role != ollama.RoleTool {
		t.Errorf("last message role = %q, want %q", last.Role, ollama.RoleTool)
	}
	if last.ToolName != "get_weather" {
		t.Errorf("tool_name = %q, want %q", last.ToolName, "get_weather")
	}
	if last.Content != tools.result {
		t.Errorf("tool content = %q, want %q", last.Content, tools.result)
	}
}

func TestRunReportsToolFailureToModelInsteadOfAborting(t *testing.T) {
	model := &fakeModel{replies: []ollama.Message{
		toolCall("get_weather", map[string]any{"city": "Atlantis"}),
		{Role: ollama.RoleAssistant, Content: "I don't know that city."},
	}}
	tools := &fakeTools{err: errors.New("unknown city")}

	got, err := agent.New(model, tools).Run(context.Background(), "weather in Atlantis?")
	if err != nil {
		t.Fatalf("Run should recover from a tool error, got: %v", err)
	}
	if got.Answer != "I don't know that city." {
		t.Errorf("answer = %q", got.Answer)
	}
	if len(got.ToolCalls) != 1 || !got.ToolCalls[0].Failed {
		t.Fatalf("expected one failed tool call, got %+v", got.ToolCalls)
	}

	second := model.seen[1]
	if last := second[len(second)-1]; last.Role != ollama.RoleTool {
		t.Errorf("failure was not fed back as a tool message: %+v", last)
	}
}

func TestRunStopsAtStepLimit(t *testing.T) {
	// A model that only ever asks for tools: without a bound this loops forever.
	model := &fakeModel{}
	for range 10 {
		model.replies = append(model.replies, toolCall("get_weather", nil))
	}
	tools := &fakeTools{result: "ok"}

	_, err := agent.New(model, tools, agent.WithMaxSteps(3)).Run(context.Background(), "loop")
	if !errors.Is(err, agent.ErrStepLimit) {
		t.Fatalf("err = %v, want ErrStepLimit", err)
	}
	if tools.calls != 3 {
		t.Errorf("tool called %d times, want 3", tools.calls)
	}
}

func TestRunStartsWithSystemPromptThenUserPrompt(t *testing.T) {
	model := &fakeModel{replies: []ollama.Message{{Role: ollama.RoleAssistant, Content: "ok"}}}

	_, err := agent.New(model, &fakeTools{}, agent.WithSystemPrompt("be terse")).Run(context.Background(), "hi")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	first := model.seen[0]
	if len(first) != 2 {
		t.Fatalf("first turn had %d messages, want 2", len(first))
	}
	if first[0].Role != ollama.RoleSystem || first[0].Content != "be terse" {
		t.Errorf("system message = %+v", first[0])
	}
	if first[1].Role != ollama.RoleUser || first[1].Content != "hi" {
		t.Errorf("user message = %+v", first[1])
	}
}
