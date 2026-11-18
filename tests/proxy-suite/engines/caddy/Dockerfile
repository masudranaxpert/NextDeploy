ARG BASE_IMAGE=lucaslorentz/caddy-docker-proxy:ci-alpine
FROM ${BASE_IMAGE}
RUN mkdir -p /srv /etc/caddy/certs && dd if=/dev/urandom of=/srv/big.bin bs=1048576 count=50 status=none
COPY caddy/Caddyfile /etc/caddy/Caddyfile
COPY certs/cert.pem certs/key.pem /etc/caddy/certs/
ENTRYPOINT ["caddy"]
CMD ["run", "--config", "/etc/caddy/Caddyfile", "--adapter", "caddyfile"]
