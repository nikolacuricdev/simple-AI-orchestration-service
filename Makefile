.PHONY: test lint run-mcp run-agent up down logs

test:
	go test ./...

lint:
	gofmt -l . && go vet ./...

# Local run without Docker: `make run-mcp` in one shell, `make run-agent` in
# another, with `ollama serve` running on the side.
run-mcp:
	go run ./cmd/mcp-server

run-agent:
	go run ./cmd/agent

up:
	docker compose up --build

down:
	docker compose down

logs:
	docker compose logs -f agent mcp-server
