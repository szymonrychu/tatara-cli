# ROADMAP.md

Component-local roadmap for tatara-cli. Phase-level platform roadmap
lives in `~/Documents/tatara/ROADMAP.md`.

Statuses: `planned`, `in progress`, `shipped`.

---

## v0.1.0 - phase 2 ship

**Status:** in progress

OIDC device flow, REST passthrough, stdio MCP server, cobra CLI skeleton.

See `~/Documents/tatara/docs/superpowers/specs/2026-05-27-tatara-cli-mvp-design.md`.

Wave 5.1 shipped: `.goreleaser.yaml` + `.github/workflows/release.yaml`. Triggers on `v*` tags.
Wave 5.2 pending: add container image push job to release workflow.
Wave 6.1 pending: push `v0.1.0` tag (after tap repo + secret are in place - see MEMORY.md).
