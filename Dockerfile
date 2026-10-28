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
ARG TARGETARCH
RUN apk add --no-cache rclone ca-certificates util-linux bash curl && \
    ARCH="${TARGETARCH:-amd64}" && \
    case "$ARCH" in \
      arm64) OMP_ARCH="arm64" ;; \
      *) OMP_ARCH="x64" ;; \
    esac && \
    curl -fsSL -o /usr/local/bin/omp "https://github.com/can1357/oh-my-pi/releases/download/v18.13.2/omp-linux-musl-${OMP_ARCH}" && \
    chmod +x /usr/local/bin/omp && \
    apk del curl
COPY --from=build /panel /usr/local/bin/panel

ENV DATA_DIR=/data
ENV WORKSPACES_ROOT=/data/workspaces
ENV DOCKER_API_VERSION=1.44
EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/panel"]
