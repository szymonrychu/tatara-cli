# tatara-cli

tatara platform CLI: OIDC device flow, REST passthrough, stdio MCP server.

Architecture: [github.com/szymonrychu/tatara/blob/main/ARCHITECTURE.md](https://github.com/szymonrychu/tatara/blob/main/ARCHITECTURE.md)

## Install

```sh
brew tap szymonrychu/tap
brew install tatara
```

## Quickstart

```sh
# authenticate (OIDC device flow)
tatara login

# call any tatara-memory endpoint
tatara raw GET /memories

# start the MCP stdio server (for Claude Code integration)
tatara mcp
```

## Build from source

```sh
git clone https://github.com/szymonrychu/tatara-cli
cd tatara-cli
make build          # binary at bin/tatara
make test           # unit tests
make lint           # golangci-lint
```
