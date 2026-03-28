run-client:
    go run ./cmd/client

run-server:
    go run ./cmd/server

build:
    go build -o bin/client ./cmd/client
    go build -o bin/server ./cmd/server
