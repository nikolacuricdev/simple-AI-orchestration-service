package mcpserver_test

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nikolacuricdev/simple-AI-orchestration-service/internal/agent"
	"github.com/nikolacuricdev/simple-AI-orchestration-service/internal/mcpserver"
)

// connect wires a real MCP server to a real MCP client over an in-memory
// transport, so these tests exercise the actual protocol without a socket.
func connect(t *testing.T) agent.Toolset {
	t.Helper()

	discard := slog.New(slog.NewTextHandler(io.Discard, nil))
	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	server := mcpserver.New("test", discard)
	serverSession, err := server.Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "test"}, nil)
	clientSession, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })

	return agent.NewMCPToolset(clientSession)
}

func TestListAdvertisesGetWeatherWithASchema(t *testing.T) {
	tools, err := connect(t).List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("got %d tools, want 1", len(tools))
	}

	tool := tools[0]
	if tool.Type != "function" {
		t.Errorf("type = %q, want %q", tool.Type, "function")
	}
	if tool.Function.Name != "get_weather" {
		t.Errorf("name = %q", tool.Function.Name)
	}
	if tool.Function.Description == "" {
		t.Error("description is empty; the model has nothing to go on")
	}
	// The schema is what the model fills the arguments in from, so it has to
	// survive the trip from the Go struct tag all the way out to the model.
	if tool.Function.Parameters == nil {
		t.Error("parameters schema is nil")
	}
}

func TestCallReturnsTheForecastAsText(t *testing.T) {
	out, err := connect(t).Call(context.Background(), "get_weather", map[string]any{"city": "Belgrade"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	for _, want := range []string{"Belgrade", "24", "sunny"} {
		if !strings.Contains(out, want) {
			t.Errorf("result %q is missing %q", out, want)
		}
	}
}

func TestCallSurfacesAToolErrorTheModelCanRead(t *testing.T) {
	_, err := connect(t).Call(context.Background(), "get_weather", map[string]any{"city": "Atlantis"})
	if err == nil {
		t.Fatal("expected an error for an unknown city")
	}
	// The tool's own message must survive the round trip — a generic
	// "tool failed" would leave the model with nothing to correct.
	if !strings.Contains(err.Error(), "Belgrade") {
		t.Errorf("error %q lost the tool's message", err)
	}
}

func TestCallUnknownToolFails(t *testing.T) {
	if _, err := connect(t).Call(context.Background(), "get_stock_price", nil); err == nil {
		t.Fatal("expected an error for an unregistered tool")
	}
}

// compile-time check that the adapter satisfies what the agent needs.
var _ agent.Toolset = (*agent.MCPToolset)(nil)
