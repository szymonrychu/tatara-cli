# Contributing to tatara-cli

Thanks for contributing. This repo uses a Makefile for all local
development tasks.

## Build

```sh
make build          # binary at bin/tatara
```

## Test

```sh
make test           # go test ./... -race -count=1
```

## Lint

```sh
make lint           # golangci-lint run ./...
```
