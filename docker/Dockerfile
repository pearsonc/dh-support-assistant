# Placeholder Dockerfile for the `app` service.
# Phase 1 replaces this with a two-stage build:
#   Stage 1: golang:1.23-alpine → compile `cmd/server` and `cmd/ingest`.
#   Stage 2: alpine:latest → copy binaries, run `cmd/server` by default.
#
# [Rule: Latest Versions] pin images to latest at Phase 1 authoring time.

FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN go build -o /out/server ./cmd/server && go build -o /out/ingest ./cmd/ingest

FROM alpine:latest
RUN apk add --no-cache ca-certificates
COPY --from=build /out/server /usr/local/bin/server
COPY --from=build /out/ingest /usr/local/bin/ingest
USER 65534:65534
ENTRYPOINT ["/usr/local/bin/server"]
