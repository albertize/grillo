# SPDX-License-Identifier: Apache-2.0
# Build the static Go executable into this temporary build context first.
FROM scratch
COPY backend /backend
USER 1000:1000
EXPOSE 8080
ENTRYPOINT ["/backend"]
