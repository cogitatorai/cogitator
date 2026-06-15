FROM golang:1.25-alpine AS builder

ARG BUILD_TAGS=""

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -tags "${BUILD_TAGS}" -o cogitator ./cmd/cogitator/

FROM alpine:3.21
RUN apk add --no-cache ca-certificates su-exec
COPY --from=builder /build/cogitator /usr/local/bin/cogitator
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh

# Unprivileged runtime user. The /data volume holds the SQLite DB and
# secrets.yaml; the entrypoint chowns it and drops to this user at startup
# (Fly mounts the volume root-owned, so a build-time chown alone is not enough).
RUN addgroup -g 1000 cogitator && \
  adduser -D -u 1000 -G cogitator cogitator && \
  mkdir -p /data && \
  chown -R cogitator:cogitator /data

EXPOSE 8484
VOLUME /data
ENV COGITATOR_WORKSPACE_PATH=/data

HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
  CMD wget -q -O /dev/null "http://localhost:${COGITATOR_SERVER_PORT:-8484}/api/health" || exit 1

ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
