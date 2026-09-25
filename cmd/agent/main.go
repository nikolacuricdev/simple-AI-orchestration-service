// Command agent is the orchestration service: it connects to an MCP server for
// tools, to Ollama for a model, and exposes the resulting agent over HTTP.
//
// This file is wiring only. Every decision it makes — which model, which tool
// server, how many steps — is configuration; the behaviour lives in
// internal/agent.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nikolacuricdev/simple-AI-orchestration-service/internal/agent"
	"github.com/nikolacuricdev/simple-AI-orchestration-service/internal/api"
	"github.com/nikolacuricdev/simple-AI-orchestration-service/internal/config"
	"github.com/nikolacuricdev/simple-AI-orchestration-service/internal/httpx"
	"github.com/nikolacuricdev/simple-AI-orchestration-service/internal/ollama"
)

const version = "1.0.0"

func main() {
	log := config.Logger("agent")
	slog.SetDefault(log)

	// ctx is cancelled on SIGINT/SIGTERM, which unwinds the HTTP server, the
	// MCP session and any in-flight model call from one place.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, log); err != nil {
		log.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, log *slog.Logger) error {
	var (
		addr          = config.String("AGENT_ADDR", ":8080")
		ollamaURL     = config.String("OLLAMA_URL", "http://localhost:11434")
		ollamaModel   = config.String("OLLAMA_MODEL", "llama3.2")
		mcpURL        = config.String("MCP_URL", "http://localhost:8081/mcp")
		maxSteps      = config.Int("AGENT_MAX_STEPS", 5)
		modelTimeout  = config.Duration("OLLAMA_TIMEOUT", 120*time.Second)
		startupWait   = config.Duration("STARTUP_RETRY_DELAY", 2*time.Second)
		startupTries  = config.Int("STARTUP_RETRIES", 30)
		requestBudget = config.Duration("AGENT_REQUEST_TIMEOUT", 5*time.Minute)
	)

	// 1. The model. Nothing is called yet; we only wait until the server is
	//    reachable, because Ollama may still be pulling the model.
	model := ollama.NewClient(ollamaURL, ollamaModel, modelTimeout)
	if err := httpx.Retry(ctx, startupTries, startupWait, log, "ollama", model.Ping); err != nil {
		return err
	}
	log.Info("ollama ready", "url", ollamaURL, "model", ollamaModel)

	// 2. The tools. One long-lived MCP session is shared by all requests; the
	//    SDK session is safe for concurrent use.
	session, err := connectMCP(ctx, mcpURL, startupTries, startupWait, log)
	if err != nil {
		return err
	}
	defer session.Close()
	log.Info("mcp connected", "url", mcpURL)

	// 3. The agent: a model plus a toolset plus a bound on how long it may think.
	weatherAgent := agent.New(
		model,
		agent.NewMCPToolset(session),
		agent.WithMaxSteps(maxSteps),
		agent.WithLogger(log),
	)

	return httpx.ListenAndServe(ctx, addr, api.NewHandler(weatherAgent, log), requestBudget, log)
}

// connectMCP opens the MCP session, retrying while the tool server boots.
func connectMCP(ctx context.Context, url string, tries int, delay time.Duration, log *slog.Logger) (*mcp.ClientSession, error) {
	client := mcp.NewClient(&mcp.Implementation{Name: "weather-agent", Version: version}, nil)

	var session *mcp.ClientSession
	err := httpx.Retry(ctx, tries, delay, log, "mcp server", func(ctx context.Context) error {
		s, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: url}, nil)
		if err != nil {
			return err
		}
		session = s
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("connect to mcp server: %w", err)
	}
	return session, nil
}
