ARG GO_IMAGE=golang:1.26-alpine
FROM ${GO_IMAGE} AS build
ENV GOTOOLCHAIN=auto
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/probe .

FROM alpine:3.20
COPY --from=build /out/probe /usr/local/bin/probe
ENTRYPOINT ["probe"]
