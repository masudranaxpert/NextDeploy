# Tailwind CSS (Dokploy-like styling)
FROM node:22-alpine AS assets
WORKDIR /app
COPY package.json ./
RUN npm install --no-audit --no-fund
COPY tailwind.config.js ./
COPY web ./web
RUN npm run build:css

# Go binary — no local Go install required on your PC
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod go.sum* ./
# Download known deps first for layer caching (best-effort; new deps resolved below)
RUN GOPROXY=https://proxy.golang.org,direct go mod download -x 2>/dev/null || true
COPY . .
COPY --from=assets /app/web/static/css/app.css ./web/static/css/app.css
# tidy after full source copy so new imports (e.g. golang.org/x/crypto) are resolved
RUN GOPROXY=https://proxy.golang.org,direct go mod tidy
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /panel .

FROM docker:27-cli
COPY --from=build /panel /usr/local/bin/panel
ENV DATA_DIR=/data
ENV WORKSPACES_ROOT=/data/workspaces
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/panel"]
