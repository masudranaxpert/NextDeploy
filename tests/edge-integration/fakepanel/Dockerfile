FROM golang:1.26-alpine AS builder
WORKDIR /src
COPY tests/edge-integration/fakepanel/main.go ./main.go
RUN CGO_ENABLED=0 go build -o /out/fakepanel ./main.go

FROM alpine:3.20
RUN apk add --no-cache ca-certificates wget
COPY --from=builder /out/fakepanel /usr/local/bin/fakepanel
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/fakepanel"]
