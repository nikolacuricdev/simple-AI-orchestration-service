// Command mcp-server serves the weather tool over MCP.
//
// It is a separate process on purpose: MCP's value is that a tool server is
// independent of any one model client. This binary has no idea an LLM exists.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nikolacuricdev/simple-AI-orchestration-service/internal/config"
	"github.com/nikolacuricdev/simple-AI-orchestration-service/internal/httpx"
	"github.com/nikolacuricdev/simple-AI-orchestration-service/internal/mcpserver"
)

const version = "1.0.0"

func main() {
	log := config.Logger("mcp-server")
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	addr := config.String("MCP_ADDR", ":8081")
	server := mcpserver.New(version, log)

	if err := httpx.ListenAndServe(ctx, addr, mcpserver.Handler(server), 30*time.Second, log); err != nil {
		log.Error("fatal", "error", err)
		os.Exit(1)
	}
}
