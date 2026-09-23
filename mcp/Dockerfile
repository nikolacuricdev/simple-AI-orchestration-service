FROM golang:1.26

WORKDIR /mcp

COPY go.mod go.sum ./

RUN go mod download

COPY main.go ./

RUN go build -o mcp .

EXPOSE 8080

CMD ["./mcp"]
