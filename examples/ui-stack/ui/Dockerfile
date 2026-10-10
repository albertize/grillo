# SPDX-License-Identifier: Apache-2.0
FROM docker.io/library/busybox:1.37@sha256:bdf57e528e45e4433820e045b29b4597825a1c9e38353532d90a01445013f82e
COPY site /www
USER 1000:1000
EXPOSE 8080
ENTRYPOINT ["/bin/busybox", "httpd", "-f", "-p", "8080", "-h", "/www"]
