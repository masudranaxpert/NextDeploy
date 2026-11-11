FROM golang:1.26-alpine AS builder
WORKDIR /src
COPY tests/edge-integration/go.mod tests/edge-integration/go.sum ./tests/edge-integration/
COPY tests/edge-integration/origin/main.go ./tests/edge-integration/origin/main.go
WORKDIR /src/tests/edge-integration
RUN go mod download github.com/gorilla/websocket && CGO_ENABLED=0 go build -o /out/origin-echo ./origin/main.go

FROM alpine:3.20
COPY --from=builder /out/origin-echo /usr/local/bin/origin-echo
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/origin-echo"]
