# tunnel-thing

Self-hosted tunnel. Expose a local service through a remote server — like ngrok but you own it.

Single TCP connection between client and server, multiplexed with [yamux](https://github.com/hashicorp/yamux) so each proxied connection gets its own stream without blocking the others.

Supports two routing modes: raw TCP on a port, or subdomain-based HTTP routing.

## Usage

Server:

```sh
just run-server -- -listen :7000
just run-server -- -listen :7000 -tokens tokens.txt -http :8080 -domain tunnel.example.com
```

Client:

```sh
# expose local :3000 on remote port 9001
just run-client -- -server host:7000 -token mytoken -local localhost:3000 -remote 9001

# or via subdomain: myapp.tunnel.example.com -> localhost:3000
just run-client -- -server host:7000 -token mytoken -local localhost:3000 -name myapp
```

## Auth

Optional. Pass `-tokens` to the server with a file of `token:label` pairs, one per line. Without it, everything is allowed through.

## Build

```sh
just build
just test
```
