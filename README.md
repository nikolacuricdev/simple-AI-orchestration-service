# Weather AI orchestration service

A minimal Go project that shows how to wire a local LLM (via [Ollama](https://ollama.com)) to a tool it can call. Built with the Go standard library only — no web framework, no ORM, nothing to learn beyond `net/http`.

## How it works

There are three small, independent pieces, all built from the same binary:

1. **Weather API** (`internal/weather`) — a plain HTTP endpoint that returns a random demo forecast for a European city. Fake data on purpose: this project is about orchestration, not real weather.
2. **AI gateway** (`internal/ai`) — sends your prompt to a local Ollama model together with a `get_weather` tool description. If the model decides it needs the weather, the gateway calls the weather API for it and sends the result back to the model.
3. **MCP server** (`internal/mcp`) — exposes the same weather lookup as an [MCP](https://modelcontextprotocol.io) tool over stdio, so any MCP-compatible client (not just this project's own AI gateway) can call it.

```text
cmd/weather-service/
  main.go       entrypoint: reads the CLI argument and dispatches to one of the three run modes below
  weather.go    runWeatherAPI() — serves the weather HTTP API (default mode)
  model.go      runModel()      — serves the AI gateway (browser chat + /chat)
  mcp.go        runMCP()        — runs the MCP stdio server

internal/weather/
  weather.go        Forecast type, the Provider interface, and the demo forecast generator
  (weather_test.go)

internal/ai/
  client.go          Client: talks to Ollama's /api/chat, runs the tool-call loop
  tool.go            get_weather tool description and its execution
  system_prompt.md   the system prompt, kept as text rather than a Go string
  (ai_test.go)

internal/httpserver/
  weather.go     GET /weather
  chat.go        GET / (chat page), GET /health, POST /chat
  httpserver.go  shared helpers: JSON responses, logging/recovery middleware
  chat.html      the browser chat page
  (httpserver_test.go)

internal/mcp/
  mcp.go   the MCP server and its get_weather tool
  (mcp_test.go)
```

Each package is one clear unit — weather domain, AI gateway, HTTP layer, MCP server — and within each package, one file per concern instead of one big file.

## Run locally

```bash
go run ./cmd/weather-service
curl 'http://localhost:8080/weather?city=Belgrade'
```

The supported cities are common European cities.

## MCP weather tool

Run the MCP server as a stdio process. It exposes one typed tool, `get_weather`, implemented with `github.com/modelcontextprotocol/go-sdk/mcp`:

```bash
go run ./cmd/weather-service mcp
```

MCP does not contain any model logic — it's just a way to expose the weather lookup as a tool to any MCP client. The browser AI agent below is the component that decides *when* to call it.

## Docker

Set the ports and local model in the `.env` file (copy from `.env.example`):

```env
WEATHER_PORT=8080
AI_PORT=8081
LOCAL_MODEL=llama3.2:1b
```

After updating the `.env` file, start the stack:

```bash
docker compose up --build
```

On the first run, it downloads the local model (`llama3.2:1b` by default, configurable with `LOCAL_MODEL`). The weather API is available at `http://localhost:8080/weather?city=Paris`, and the browser chat is available at `http://localhost:8081`.

The AI gateway and Ollama communicate over the Compose network. Model data is kept in the `ollama-data` volume.

Prompt from the terminal:

```bash
curl -s http://localhost:8081/chat \
	-H 'Content-Type: application/json' \
	-d '{"prompt":"What is the weather in Belgrade?"}'
```

Or open `http://localhost:8081` in a browser and enter the prompt in the chat form. The model autonomously decides whether to call `get_weather` for a European city, answer the capital of a European country, or reject unrelated topics.

To run the MCP process interactively through Compose:

```bash
docker compose run --rm mcp-server
```

If ports are already in use, change `WEATHER_PORT` and `AI_PORT` directly in the `.env` file.


### ollama api docs
`https://github.com/ollama/ollama/blob/main/docs/api.md`

### ollama tool calling
`https://github.com/ollama/ollama/blob/main/docs/capabilities/tool-calling.mdx`

### ollama api types in go
`https://github.com/ollama/ollama/blob/main/api/types.go`

### go-sdk modelcontextprotocol
`https://github.com/modelcontextprotocol/go-sdk/tree/main`