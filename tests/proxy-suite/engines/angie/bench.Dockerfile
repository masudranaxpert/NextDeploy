ARG BASE_IMAGE=docker.angie.software/angie:1.12.4
FROM ${BASE_IMAGE}
COPY angie/bench.angie.conf /etc/angie/angie.conf
COPY certs/cert.pem certs/key.pem /etc/angie/certs/
