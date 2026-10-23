FROM docker:27-cli
RUN apk add --no-cache rclone ca-certificates
COPY ./panel /usr/local/bin/panel

ENV DATA_DIR=/data
ENV WORKSPACES_ROOT=/data/workspaces
ENV DOCKER_API_VERSION=1.44
EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/panel"]
