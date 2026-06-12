SHELL := /usr/bin/env bash
.SHELLFLAGS := -eu -o pipefail -c

REGISTRY ?= harbor.szymonrichert.pl
IMAGE_NAME ?= containers/tatara-cli
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

IMAGE_REF := $(REGISTRY)/$(IMAGE_NAME):$(VERSION)

.PHONY: all lint test build image push tidy fmt clean

all: lint test build

tidy:
	go mod tidy

fmt:
	gofmt -s -w .
	goimports -w -local github.com/szymonrychu/tatara-cli .

lint:
	golangci-lint config verify
	golangci-lint run ./... || [ $$? -eq 5 ]

test:
	go test ./... -race -count=1

build:
	CGO_ENABLED=0 go build \
		-trimpath \
		-ldflags "-s -w \
		  -X github.com/szymonrychu/tatara-cli/internal/version.Version=$(VERSION) \
		  -X github.com/szymonrychu/tatara-cli/internal/version.Commit=$(COMMIT) \
		  -X github.com/szymonrychu/tatara-cli/internal/version.Date=$(DATE)" \
		-o bin/tatara \
		./cmd/tatara

image:
	docker buildx build \
		--platform=linux/amd64 \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT=$(COMMIT) \
		--build-arg DATE=$(DATE) \
		-t $(IMAGE_REF) \
		--load \
		.

push: image
	docker push $(IMAGE_REF)

clean:
	rm -rf bin dist
