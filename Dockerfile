# Multi-stage build for NextDeploy Panel
FROM golang:1.24-alpine AS build
ARG TARGETOS=linux
ARG TARGETARCH=amd64
WORKDIR /src

# Download dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source code and embedded web assets
COPY . .

# Compile static Go binary natively with target architecture (bypasses slow QEMU emulation)
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /panel .

# Runtime container
FROM docker:27-cli
RUN apk add --no-cache rclone ca-certificates util-linux bash libstdc++
COPY --from=build /panel /usr/local/bin/panel
COPY bin/omp /usr/local/bin/omp

ENV DATA_DIR=/data
ENV WORKSPACES_ROOT=/data/workspaces
ENV DOCKER_API_VERSION=1.44
EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/panel"]
