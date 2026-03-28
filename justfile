run-client *ARGS:
    go run ./cmd/client {{ARGS}}

run-server *ARGS:
    go run ./cmd/server {{ARGS}}

e2e:
    #!/usr/bin/env bash
    trap 'kill 0' EXIT
    go run ./cmd/server -listen :7000 -secret s -http :8080 -domain localhost &
    sleep 1
    go run ./cmd/client -server localhost:7000 -secret s -tunnel myapp=localhost:3000 -v

build:
    go build -o bin/client ./cmd/client
    go build -o bin/server ./cmd/server

test:
    go test ./...
