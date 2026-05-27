# syntax=docker/dockerfile:1.7

ARG GO_VERSION=1.25
FROM golang:${GO_VERSION}-alpine AS builder

WORKDIR /src
RUN apk add --no-cache git ca-certificates

COPY go.mod ./
RUN go mod download

COPY . .

RUN mkdir -p /out

ARG VERSION=dev
ARG COMMIT=unknown
ARG DATE=unknown

RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags "-s -w \
      -X github.com/szymonrychu/tatara-cli/internal/version.Version=${VERSION} \
      -X github.com/szymonrychu/tatara-cli/internal/version.Commit=${COMMIT} \
      -X github.com/szymonrychu/tatara-cli/internal/version.Date=${DATE}" \
    -o /out/tatara \
    ./cmd/tatara

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /out/tatara /tatara
USER nonroot:nonroot
ENTRYPOINT ["/tatara"]
