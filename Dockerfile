# Tailwind CSS
FROM node:22-alpine AS assets
WORKDIR /app
COPY package.json ./
RUN npm install --no-audit --no-fund
COPY tailwind.config.js ./
COPY web ./web
RUN npm run build:css

# Go binary — no local Go install required on your PC
FROM golang:1.24-alpine AS build
WORKDIR /src

# Copy dependency files first for better layer caching
COPY go.mod go.sum* ./

# Download dependencies (cached unless go.mod/go.sum changes)
RUN GOPROXY=https://proxy.golang.org,direct go mod download -x

# Copy source code (this layer rebuilds when code changes)
COPY . .

# Copy compiled CSS from assets stage
COPY --from=assets /app/web/static/css/app.css ./web/static/css/app.css

# Build the binary
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /panel .

# Runtime: Docker CLI + rclone (volume ZIP restore uses `docker run alpine` + apk add unzip in that container, not this image)
FROM docker:27-cli
RUN apk add --no-cache rclone ca-certificates

# Copy the compiled binary
COPY --from=build /panel /usr/local/bin/panel

ENV DATA_DIR=/data
ENV WORKSPACES_ROOT=/data/workspaces
EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/panel"]
