package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nikolacuricdev/simple-AI-orchestration-service/internal/ollama"
)

// MCPToolset adapts an MCP server to the Toolset the agent loop expects.
//
// This adapter is the entire integration between MCP and the model: MCP
// describes tools with a JSON Schema, and so does the Ollama tool API, so the
// conversion is mostly a rename. Everything the agent knows about MCP lives in
// this file — swap it for a different Toolset and the loop is unchanged.
type MCPToolset struct {
	session *mcp.ClientSession
}

// NewMCPToolset wraps a connected MCP client session.
func NewMCPToolset(session *mcp.ClientSession) *MCPToolset {
	return &MCPToolset{session: session}
}

// List asks the MCP server what it can do and translates the answer into tool
// descriptions the model understands.
func (t *MCPToolset) List(ctx context.Context) ([]ollama.Tool, error) {
	var tools []ollama.Tool

	// session.Tools is an iterator that follows the server's pagination, so a
	// server with more tools than fit in one page still works.
	for tool, err := range t.session.Tools(ctx, nil) {
		if err != nil {
			return nil, fmt.Errorf("list mcp tools: %w", err)
		}
		tools = append(tools, ollama.Tool{
			Type: "function",
			Function: ollama.ToolFunction{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.InputSchema,
			},
		})
	}
	return tools, nil
}

// Call runs one MCP tool and flattens its result to text.
//
// There are two distinct failure modes here, and conflating them is a common
// bug: a transport error means MCP itself broke, while IsError means the tool
// ran and reported a problem. Both are returned as errors so the agent can
// relay them to the model, but only the second carries the tool's own message.
func (t *MCPToolset) Call(ctx context.Context, name string, args map[string]any) (string, error) {
	result, err := t.session.CallTool(ctx, &mcp.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	if err != nil {
		return "", fmt.Errorf("call mcp tool %q: %w", name, err)
	}

	text := contentText(result.Content)

	if result.IsError {
		if text == "" {
			text = "the tool reported an error"
		}
		return "", errors.New(text)
	}
	return text, nil
}

// contentText renders MCP content blocks as the plain text a model can read.
// Text blocks pass through; anything else (images, embedded resources) is
// serialised as JSON rather than silently dropped.
func contentText(content []mcp.Content) string {
	var b strings.Builder
	for _, c := range content {
		switch v := c.(type) {
		case *mcp.TextContent:
			b.WriteString(v.Text)
		default:
			if encoded, err := json.Marshal(v); err == nil {
				b.Write(encoded)
			}
		}
	}
	return b.String()
}
