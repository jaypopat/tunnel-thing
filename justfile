run-client *ARGS:
    go run ./cmd/client {{ARGS}}

run-server *ARGS:
    go run ./cmd/server {{ARGS}}

build:
    go build -o bin/client ./cmd/client
    go build -o bin/server ./cmd/server

test:
    go test ./...
