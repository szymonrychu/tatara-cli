# ROADMAP.md

Component-local roadmap for tatara-cli. Phase-level platform roadmap
lives in `~/Documents/tatara/ROADMAP.md`.

Statuses: `planned`, `in progress`, `shipped`.

---

## v0.1.0 - phase 2 ship

**Status:** code-complete on main 2026-05-27, awaiting release.

OIDC device flow, REST passthrough, stdio MCP server, full cobra CLI
(`login`, `logout`, `raw`, `mcp`, `mcp-config`). 13 MCP tools mapping
tatara-memory REST 1:1. CI green.

See `~/Documents/tatara/docs/superpowers/specs/2026-05-27-tatara-cli-mvp-design.md`.

Release pipeline ready (goreleaser + Homebrew tap + Harbor container).
Pushing `v0.1.0` is blocked on three preconditions documented in
MEMORY.md: tap repo creation, `HOMEBREW_TAP_GITHUB_TOKEN`,
`HARBOR_USERNAME`/`HARBOR_PASSWORD` secrets. The user will handle the
actual v0.1.0 cut when those are in place.

## v0.1.1 - follow-ups

**Status:** planned

- Tighten `.golangci.yml` schema for golangci-lint v2.12+ (current config
  fails `config verify`; `run` still tolerates the v1 leftovers).
- Drive end-to-end MCP smoke from a test harness once a canonical Go MCP
  client lands.
