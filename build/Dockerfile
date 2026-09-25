# One Dockerfile for both binaries: the build stage is identical, and BINARY
# picks which cmd/ to compile. Multi-stage keeps the Go toolchain out of the
# image that ships — the runtime is a ~20 MB base instead of a ~1.1 GB one.

FROM golang:1.26 AS build

ARG BINARY

WORKDIR /src

# Dependencies are copied first so `go mod download` is cached until go.mod or
# go.sum actually change.
COPY go.mod go.sum ./
RUN go mod download

COPY cmd/ ./cmd/
COPY internal/ ./internal/

# CGO off produces a static binary that runs on the distroless base below.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/service ./cmd/${BINARY}

# distroless/static has CA certificates and a nonroot user, and nothing else:
# no shell, no package manager, nothing for an attacker to pivot into.
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/service /service

USER nonroot:nonroot

ENTRYPOINT ["/service"]
