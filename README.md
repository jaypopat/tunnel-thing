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

Both server and client accept `-v` for verbose (debug-level) logging.

Client (flags):

```sh
# subdomain: myapp.tunnel.example.com → localhost:3000
bin/client -server host:7000 -secret mysecret -tunnel myapp=localhost:3000

# raw TCP: remote port 9001 → localhost:3000
bin/client -server host:7000 -secret mysecret -tunnel 9001=localhost:3000

# multiple tunnels at once
bin/client -server host:7000 -secret mysecret \
  -tunnel myapp=localhost:3000 \
  -tunnel 5432=localhost:5432
```

Client (TOML config):

```sh
bin/client -config config.toml
```

If no `-tunnel` or `-config` flags are given, the client automatically looks for a `config.toml` in the current directory.

```toml
server = "host:7000"
secret = "mysecret"

[[tunnels]]
name = "myapp"
local = "localhost:3000"

[[tunnels]]
port = 5432
local = "localhost:5432"
```

## Auth

Optional. Pass the same `-secret` value to both server and client. Without it, all connections are accepted.

## Build

```sh
just build
just test
```
