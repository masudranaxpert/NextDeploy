# syntax=docker/dockerfile:1.6

FROM node:22-alpine AS assets
WORKDIR /app
COPY package.json package-lock.json ./
RUN --mount=type=cache,target=/root/.npm \
    npm ci --no-audit --no-fund
COPY tailwind.config.js ./
COPY web ./web
RUN npm run build:css

FROM golang:1.24-alpine AS build
WORKDIR /src

ENV GOPROXY=https://proxy.golang.org,direct
ENV GOFLAGS=-mod=mod
ENV GOMODCACHE=/go/pkg/mod
ENV GOCACHE=/root/.cache/go-build

COPY go.mod go.sum* ./

# Retry because proxy.golang.org occasionally closes a stream mid-download
# (see moby/moby#49513, golang/go#46237). Cache mounts keep modules across rebuilds.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    set -eu; \
    for attempt in 1 2 3 4 5; do \
      echo "go mod download attempt $attempt"; \
      if go mod download; then exit 0; fi; \
      echo "attempt $attempt failed; retrying"; \
      sleep $(( attempt * 3 )); \
    done; \
    echo "go mod download failed after 5 attempts" >&2; exit 1

COPY . .
COPY --from=assets /app/web/static/css/app.css ./web/static/css/app.css

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /panel .

FROM docker:27-cli
RUN apk add --no-cache rclone ca-certificates
COPY --from=build /panel /usr/local/bin/panel

ENV DATA_DIR=/data
ENV WORKSPACES_ROOT=/data/workspaces
EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/panel"]
