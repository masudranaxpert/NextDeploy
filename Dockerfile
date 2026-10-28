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

# omp (the agent engine) is fetched from its GitHub release at build time, so CI
# and local builds need no binary in the repo. Upgrade = rebuild; pin with
#   --build-arg OMP_VERSION=v18.13.2   (default: latest release)
FROM alpine:3.20 AS omp
ARG TARGETARCH=amd64
ARG OMP_VERSION=latest
RUN apk add --no-cache curl ca-certificates \
 && case "$TARGETARCH" in amd64) A=x64;; arm64) A=arm64;; *) echo "unsupported arch $TARGETARCH" >&2; exit 1;; esac \
 && if [ "$OMP_VERSION" = latest ]; then BASE=https://github.com/can1357/oh-my-pi/releases/latest/download; else BASE=https://github.com/can1357/oh-my-pi/releases/download/$OMP_VERSION; fi \
 && curl -fsSL --retry 5 --retry-all-errors --retry-delay 2 "$BASE/omp-linux-musl-$A" -o /omp \
 && curl -fsSL --retry 5 --retry-all-errors --retry-delay 2 "$BASE/SHA256SUMS.txt" -o /SHA256SUMS.txt \
 && echo "$(grep " omp-linux-musl-$A\$" /SHA256SUMS.txt | cut -d' ' -f1)  /omp" | sha256sum -c - \
 && chmod +x /omp

# Runtime container
FROM docker:27-cli
RUN apk add --no-cache rclone ca-certificates util-linux bash libstdc++ openssh-client python3 curl jq git
COPY --from=build /panel /usr/local/bin/panel
COPY --from=omp /omp /usr/local/bin/omp

ENV DATA_DIR=/data
ENV WORKSPACES_ROOT=/data/workspaces
ENV DOCKER_API_VERSION=1.44
EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/panel"]
