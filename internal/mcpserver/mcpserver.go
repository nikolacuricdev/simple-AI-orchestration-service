// Package mcpserver defines the MCP server and the tools it exposes.
//
// MCP (modelcontextprotocol.io) is the contract between a tool provider and
// any model client. Nothing in this package knows that an LLM exists: it
// publishes typed, self-describing tools, and any MCP client — this project's
// agent, Claude Desktop, an IDE — can discover and call them.
//
// SDK: https://github.com/modelcontextprotocol/go-sdk
package mcpserver

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nikolacuricdev/simple-AI-orchestration-service/internal/weather"
)

// WeatherInput is the tool's argument struct.
//
// The SDK derives the tool's JSON Schema from these fields, and the agent
// forwards that schema to the model verbatim. So the `jsonschema` tag below is
// not documentation for humans — it is the instruction the model reads when
// deciding what to put in the argument.
type WeatherInput struct {
	City string `json:"city" jsonschema:"name of the city to get the weather for, for example Belgrade or Paris"`
}

// New builds the MCP server with its tools registered.
func New(version string, log *slog.Logger) *mcp.Server {
	server := mcp.NewServer(
		&mcp.Implementation{Name: "weather-mcp-server", Version: version},
		nil,
	)

	// mcp.AddTool infers the input and output schemas from the handler's
	// generic parameters, so the tool description and the Go types can never
	// drift apart.
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_weather",
		Description: "Get the current weather for a European city. Returns temperature in Celsius and a short condition.",
	}, getWeather(log))

	return server
}

// getWeather is the tool handler. Returning (nil, out, nil) lets the SDK encode
// out as both the structured result and its text rendering.
func getWeather(log *slog.Logger) mcp.ToolHandlerFor[WeatherInput, weather.Report] {
	return func(ctx context.Context, req *mcp.CallToolRequest, in WeatherInput) (*mcp.CallToolResult, weather.Report, error) {
		log.InfoContext(ctx, "get_weather", "city", in.City)

		report, err := weather.Lookup(in.City)
		if err != nil {
			// A bad argument is a *tool* error, not a protocol error: it is
			// reported in the result so the model can read it and retry.
			// Returning a Go error instead would surface as an MCP failure,
			// which the model never gets to see.
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
			}, weather.Report{}, nil
		}

		return nil, report, nil
	}
}

// Handler exposes the server over Streamable HTTP.
//
// Stateless mode means every request is self-contained, so the server can be
// scaled or restarted without clients losing a session — the right default for
// a tool server behind a network.
func Handler(server *mcp.Server) http.Handler {
	return mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{JSONResponse: true, Stateless: true},
	)
}
