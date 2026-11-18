ARG BASE_IMAGE=ferronserver/ferron:3.0.0-rc.11-alpine
FROM ${BASE_IMAGE}
USER root
RUN mkdir -p /etc/ferron/certs
COPY ferron/bench.ferron.conf /etc/ferron/ferron.conf
COPY certs/cert.pem certs/key.pem /etc/ferron/certs/
RUN chmod 644 /etc/ferron/ferron.conf /etc/ferron/certs/cert.pem /etc/ferron/certs/key.pem
USER nobody
CMD ["/usr/local/bin/ferron", "run", "-c", "/etc/ferron/ferron.conf"]
