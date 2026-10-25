# Multi-stage build for NextDeploy Panel
FROM golang:1.24-alpine AS build
WORKDIR /src

# Download dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source code and embedded web assets
COPY . .

# Compile static Go binary with embedded web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /panel .

# Runtime container
FROM docker:27-cli
RUN apk add --no-cache rclone ca-certificates util-linux bash
COPY --from=build /panel /usr/local/bin/panel

ENV DATA_DIR=/data
ENV WORKSPACES_ROOT=/data/workspaces
ENV DOCKER_API_VERSION=1.44
EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/panel"]
