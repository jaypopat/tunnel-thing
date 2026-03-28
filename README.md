# tunnel-thing

Self-hosted tunnel. Expose a local service through a remote server — like ngrok but you own it.

Single TCP connection between client and server, multiplexed with [yamux](https://github.com/hashicorp/yamux) so each proxied connection gets its own stream without blocking the others.

Supports two routing modes: raw TCP on a port, or subdomain-based HTTP routing.

## Usage

Server:

```sh
# open mode (no auth)
bin/server -listen :7000

# with auth + subdomain routing
bin/server -listen :7000 -secret mysecret -http :80 -domain tunnel.example.com
```

Client:

```sh
# expose local :3000 on remote port 9001
bin/client -server host:7000 -secret mysecret -local localhost:3000 -remote 9001

# or via subdomain: myapp.tunnel.example.com → localhost:3000
bin/client -server host:7000 -secret mysecret -local localhost:3000 -name myapp
```

## Auth

Optional. Pass the same `-secret` value to both server and client. Without it, all connections are accepted.

## Build

```sh
just build
just test
```
