package main

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type WeatherInput struct {
	City string `json:"city" jsonschema:"city for which to get the weather"`
}

type WeatherOutput struct {
	City        string `json:"city"`
	Temperature int    `json:"temperature"`
	Condition   string `json:"condition"`
}

func GetWeather(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input WeatherInput,
) (*mcp.CallToolResult, WeatherOutput, error) {

	log.Printf("get_weather called for city=%s", input.City)

	// Demo response.
	// In a real application, this would be an HTTP request
	// to the weather API.
	return nil, WeatherOutput{
		City:        input.City,
		Temperature: 24,
		Condition:   "sunny",
	}, nil
}

func main() {
	server := mcp.NewServer(
		&mcp.Implementation{
			Name:    "weather-mcp-server",
			Version: "1.0.0",
		},
		nil,
	)

	mcp.AddTool(
		server,
		&mcp.Tool{
			Name:        "get_weather",
			Description: "Get the current weather for a city.",
		},
		GetWeather,
	)

	handler := mcp.NewStreamableHTTPHandler(
		func(r *http.Request) *mcp.Server {
			return server
		},
		&mcp.StreamableHTTPOptions{
			JSONResponse: true,
			Stateless:    true,
		},
	)

	log.Println("MCP server listening on :8080")

	if err := http.ListenAndServe(":8080", handler); err != nil {
		log.Fatal(fmt.Errorf("MCP server failed: %w", err))
	}
}
