package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	ollamaURL = "http://simple-ai-service-ollama:11434"
	mcpURL    = "http://simple-ai-service-mcp:8080/mcp"
	model     = "llama3.2"
)

type OllamaMessage struct {
	Role      string           `json:"role"`
	Content   string           `json:"content,omitempty"`
	ToolCalls []OllamaToolCall `json:"tool_calls,omitempty"`
}

type OllamaToolCall struct {
	Function OllamaFunctionCall `json:"function"`
}

type OllamaFunctionCall struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type OllamaTool struct {
	Type     string             `json:"type"`
	Function OllamaToolFunction `json:"function"`
}

type OllamaToolFunction struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"`
}

type OllamaChatRequest struct {
	Model    string          `json:"model"`
	Messages []OllamaMessage `json:"messages"`
	Tools    []OllamaTool    `json:"tools,omitempty"`
	Stream   bool            `json:"stream"`
}

type OllamaChatResponse struct {
	Message OllamaMessage `json:"message"`
}

type App struct {
	mcpSession *mcp.ClientSession
}

func main() {
	ctx := context.Background()

	/*
		------------------------------------------------
		1. Connect to MCP server
		------------------------------------------------
	*/

	mcpClient := mcp.NewClient(
		&mcp.Implementation{
			Name:    "my-application",
			Version: "1.0.0",
		},
		nil,
	)

	mcpTransport := &mcp.StreamableClientTransport{
		Endpoint: mcpURL,
	}

	session, err := mcpClient.Connect(ctx, mcpTransport, nil)
	if err != nil {
		log.Fatalf("failed to connect to MCP server: %v", err)
	}

	defer session.Close()

	log.Println("Connected to MCP server")

	/*
		------------------------------------------------
		2. Start HTTP application
		------------------------------------------------
	*/

	app := &App{
		mcpSession: session,
	}

	http.HandleFunc("/", app.handlePrompt)

	log.Println("Application listening on :8080")

	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatal(err)
	}
}

func (a *App) handlePrompt(w http.ResponseWriter, r *http.Request) {

	ctx := r.Context()

	prompt := r.URL.Query().Get("prompt")

	if prompt == "" {
		http.Error(
			w,
			"missing query parameter: prompt",
			http.StatusBadRequest,
		)
		return
	}

	log.Printf("Received prompt: %s", prompt)

	/*
		------------------------------------------------
		3. Get tools from MCP server
		------------------------------------------------
	*/

	tools, err := a.mcpSession.ListTools(ctx, nil)
	if err != nil {
		http.Error(
			w,
			fmt.Sprintf("failed to list MCP tools: %v", err),
			http.StatusInternalServerError,
		)
		return
	}

	/*
		------------------------------------------------
		4. Convert MCP tools -> Ollama tools
		------------------------------------------------
	*/

	ollamaTools := make([]OllamaTool, 0, len(tools.Tools))

	for _, tool := range tools.Tools {

		ollamaTools = append(
			ollamaTools,
			OllamaTool{
				Type: "function",
				Function: OllamaToolFunction{
					Name:        tool.Name,
					Description: tool.Description,
					Parameters:  tool.InputSchema,
				},
			},
		)
	}

	/*
		------------------------------------------------
		5. Ask Ollama
		------------------------------------------------
	*/

	messages := []OllamaMessage{
		{
			Role:    "user",
			Content: prompt,
		},
	}

	for {

		response, err := callOllama(
			ctx,
			messages,
			ollamaTools,
		)

		if err != nil {
			http.Error(
				w,
				fmt.Sprintf("Ollama error: %v", err),
				http.StatusInternalServerError,
			)
			return
		}

		assistantMessage := response.Message

		/*
			------------------------------------------------
			6. Ollama did not request a tool
			------------------------------------------------

			This means the LLM has finished its response.
		*/

		if len(assistantMessage.ToolCalls) == 0 {

			w.Header().Set(
				"Content-Type",
				"application/json",
			)

			json.NewEncoder(w).Encode(
				map[string]string{
					"response": assistantMessage.Content,
				},
			)

			return
		}

		/*
			------------------------------------------------
			7. Ollama requests one or more tools
			------------------------------------------------
		*/

		messages = append(
			messages,
			assistantMessage,
		)

		for _, toolCall := range assistantMessage.ToolCalls {

			log.Printf(
				"Ollama requested tool: %s",
				toolCall.Function.Name,
			)

			/*
				--------------------------------------------
				8. Call MCP tool
				--------------------------------------------
			*/

			result, err := a.mcpSession.CallTool(
				ctx,
				&mcp.CallToolParams{
					Name:      toolCall.Function.Name,
					Arguments: toolCall.Function.Arguments,
				},
			)

			if err != nil {
				http.Error(
					w,
					fmt.Sprintf(
						"MCP tool error: %v",
						err,
					),
					http.StatusInternalServerError,
				)
				return
			}

			/*
				--------------------------------------------
				9. Convert MCP result -> text
				--------------------------------------------
			*/

			toolResult := extractMCPResult(result)

			log.Printf(
				"MCP tool result: %s",
				toolResult,
			)

			/*
				--------------------------------------------
				10. Send tool result back to Ollama
				--------------------------------------------
			*/

			messages = append(
				messages,
				OllamaMessage{
					Role:    "tool",
					Content: toolResult,
				},
			)
		}

		/*
			--------------------------------------------
			11. Loop again
			--------------------------------------------

			Ollama now sees:

			user
			  ↓
			assistant tool_call
			  ↓
			tool result

			and can generate the final answer
			or request another tool.
		*/
	}
}

func callOllama(
	ctx context.Context,
	messages []OllamaMessage,
	tools []OllamaTool,
) (*OllamaChatResponse, error) {

	requestBody := OllamaChatRequest{
		Model:    model,
		Messages: messages,
		Tools:    tools,
		Stream:   false,
	}

	body, err := json.Marshal(requestBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		ollamaURL+"/api/chat",
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, err
	}

	req.Header.Set(
		"Content-Type",
		"application/json",
	)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(
			"Ollama returned %s: %s",
			resp.Status,
			string(responseBody),
		)
	}

	var result OllamaChatResponse

	if err := json.Unmarshal(
		responseBody,
		&result,
	); err != nil {
		return nil, err
	}

	return &result, nil
}

func extractMCPResult(
	result *mcp.CallToolResult,
) string {

	if result.IsError {
		return "Tool returned an error."
	}

	var output string

	for _, content := range result.Content {

		switch c := content.(type) {

		case *mcp.TextContent:
			output += c.Text

		default:
			data, _ := json.Marshal(c)
			output += string(data)
		}
	}

	return output
}

func init() {
	/*
		Make sure the application doesn't buffer logs
		inside Docker.
	*/
	log.SetOutput(os.Stdout)
}
