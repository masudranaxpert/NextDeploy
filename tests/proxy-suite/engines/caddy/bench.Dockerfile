ARG BASE_IMAGE=lucaslorentz/caddy-docker-proxy:ci-alpine
FROM ${BASE_IMAGE}
COPY caddy/bench.Caddyfile /etc/caddy/Caddyfile
COPY certs/cert.pem certs/key.pem /etc/caddy/certs/
ENTRYPOINT ["caddy"]
CMD ["run", "--config", "/etc/caddy/Caddyfile", "--adapter", "caddyfile"]
