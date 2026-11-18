ARG BASE_IMAGE=docker.angie.software/angie:1.12.4
FROM ${BASE_IMAGE}
RUN mkdir -p /srv /etc/angie/certs && dd if=/dev/urandom of=/srv/big.bin bs=1048576 count=50 status=none
COPY angie/angie.conf /etc/angie/angie.conf
COPY certs/cert.pem certs/key.pem /etc/angie/certs/
