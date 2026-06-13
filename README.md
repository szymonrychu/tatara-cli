# tatara-cli

> Part of the tatara platform - see ARCHITECTURE.md in the tatara repo.

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

# show local auth state and resolved backend URLs (no network calls)
tatara status

# call any tatara-memory endpoint
tatara raw GET /memories

# call the operator or chat backend with the same auth
tatara raw --target operator GET /tasks/foo

# start the MCP stdio server (for Claude Code integration)
tatara mcp

# write an MCP server config into the given directory
tatara mcp-config ~/.config/claude

# clear stored credentials
tatara logout
```

## Build from source

```sh
git clone https://github.com/szymonrychu/tatara-cli
cd tatara-cli
make build          # binary at bin/tatara
make test           # unit tests
make lint           # golangci-lint
```

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for build, test, and lint instructions.
