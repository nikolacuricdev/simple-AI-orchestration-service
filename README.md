# Simple AI orchestration service

A minimal, runnable example of the four things every LLM integration needs:

1. **Talking to a model** — `internal/ollama`
2. **Describing tools to it** — `internal/mcpserver`
3. **Discovering and calling those tools over MCP** — `internal/agent/mcptools.go`
4. **The agent loop that ties them together** — `internal/agent/agent.go`

Go standard library plus the official [MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk). No framework, no ORM, no magic. The demo tool returns fake weather, because the point is the orchestration, not the forecast.

> **📖 [Building an AI Agent in Go: Tool Orchestration with MCP and a Local LLM](docs/building-an-ai-agent-in-go.md)** — the illustrated, step-by-step walkthrough of how and why this is built the way it is. Start there if you came to learn rather than to run.

## The agent loop

Everything else in this repo exists to serve these thirty lines ([`internal/agent/agent.go`](internal/agent/agent.go)):

```text
                 ┌──────────────────────────────────────────┐
                 │                                          │
                 ▼                                          │
 system + user ──► ask the model ──► tool calls? ──no──► final answer
    prompt                               │
                                        yes
                                         │
                                         ▼
                                  run each tool,
                                  append its output ────────┘
                                  as a "tool" message
```

The parts that are easy to get wrong, and are called out in the code:

| Concern | What this repo does |
|---|---|
| Runaway loops | Bounded by `WithMaxSteps`; returns `ErrStepLimit` instead of spinning |
| Tool failures | Fed back to the model as a tool result, so it can retry or explain — not turned into a 500 |
| Multiple tool calls per turn | Each result carries `tool_name`, so the model can match results to calls |
| Cancellation | `r.Context()` flows into the model call and the MCP call; a disconnecting client unwinds everything |
| Testability | The loop depends on the `Model` and `Toolset` interfaces only, so `internal/agent/agent_test.go` tests it with no model and no network |

## Layout

```text
cmd/agent/          wiring: config, wait for dependencies, build the agent, serve HTTP
cmd/mcp-server/     wiring: build the MCP server, serve HTTP

internal/agent/
  agent.go          the agent loop, plus the Model and Toolset interfaces it needs
  mcptools.go       MCP  ->  Toolset adapter: the whole MCP/model integration

internal/ollama/    the model client: Message, ToolCall, Tool, and one Chat call
internal/mcpserver/ the MCP server and its get_weather tool definition
internal/weather/   the tool's domain logic (fake data, real error path)
internal/api/       HTTP surface: POST /chat, GET /chat?prompt=, GET /healthz
internal/config/    environment configuration and the logger
internal/httpx/     shared server boilerplate: timeouts, graceful shutdown, startup retry
```

Two binaries, one module. The MCP server is a **separate process** on purpose: that is MCP's whole point — a tool server that no model client owns. `cmd/mcp-server` does not import anything AI-related.

## Run with Docker

```bash
cp .env.example .env     # optional: change the port or the model
docker compose up --build
```

The first run pulls the model (~2 GB for `llama3.2`), which takes a few minutes. The agent waits for the pull to finish before it starts serving.

```bash
curl -s localhost:8080/chat \
  -H 'Content-Type: application/json' \
  -d '{"prompt":"What is the weather in Belgrade?"}' | jq
```

```json
{
  "answer": "It is 24°C and sunny in Belgrade.",
  "steps": 2,
  "tool_calls": [
    {
      "step": 1,
      "name": "get_weather",
      "args": { "city": "Belgrade" },
      "result": "{\"city\":\"Belgrade\",\"temperature_c\":24,\"condition\":\"sunny\"}"
    }
  ]
}
```

The `tool_calls` trace is in the response deliberately: it shows the model deciding to call the tool, rather than just the sentence it produced afterwards.

Ask about a city the tool does not know and you can watch the recovery path — the tool fails, the error goes back to the model, and the model answers anyway:

```bash
curl -s 'localhost:8080/chat?prompt=weather in Atlantis' | jq
```

## Run without Docker

You need [Ollama](https://ollama.com) running locally and the model pulled:

```bash
ollama serve &
ollama pull llama3.2

make run-mcp     # terminal 1 — tool server on :8081
make run-agent   # terminal 2 — agent on :8080
```

## Test

```bash
make test
```

The tests use fakes and an in-memory MCP transport, so they need neither Docker nor a model.

## Configuration

Every setting is an environment variable with a working default.

| Variable | Default | Used by |
|---|---|---|
| `AGENT_ADDR` | `:8080` | agent |
| `OLLAMA_URL` | `http://localhost:11434` | agent |
| `OLLAMA_MODEL` | `llama3.2` | agent |
| `OLLAMA_TIMEOUT` | `120s` | agent |
| `MCP_URL` | `http://localhost:8081/mcp` | agent |
| `AGENT_MAX_STEPS` | `5` | agent |
| `AGENT_REQUEST_TIMEOUT` | `5m` | agent |
| `STARTUP_RETRIES` | `30` | agent |
| `STARTUP_RETRY_DELAY` | `2s` | agent |
| `MCP_ADDR` | `:8081` | mcp-server |
| `LOG_LEVEL` | `info` | both |

`.env` is read by Docker Compose only; the binaries read the environment directly.

## Pointing another MCP client at the tool server

The tool server is plain MCP over Streamable HTTP, so it is not tied to this agent. Compose keeps it on the internal network; publish the port and any MCP client can reach `http://localhost:8081/mcp`:

```bash
docker compose run --rm -p 8081:8081 mcp-server
```

## Notes on the model

`llama3.2` is the default because it is small and supports tool calling. Smaller variants (`llama3.2:1b`) run faster but call tools noticeably worse — they tend to answer from memory instead of invoking `get_weather`. If the demo skips the tool, try a larger model before suspecting the code.

## References

- [Ollama API](https://github.com/ollama/ollama/blob/main/docs/api.md) · [tool calling](https://github.com/ollama/ollama/blob/main/docs/capabilities/tool-calling.mdx)
- [Model Context Protocol](https://modelcontextprotocol.io) · [Go SDK](https://github.com/modelcontextprotocol/go-sdk)
