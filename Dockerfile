FROM node:20-alpine AS dashboard
WORKDIR /build
COPY dashboard/package.json dashboard/package-lock.json ./
RUN npm ci
COPY dashboard/ .
RUN npm run build

FROM golang:1.25-alpine AS builder
RUN apk add --no-cache git
ARG BUILD_TAGS=""
WORKDIR /build
COPY .git .git
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ .
RUN VERSION=$(git describe --tags --match 'v[0-9]*' --always 2>/dev/null || echo dev) && \
  CGO_ENABLED=0 go build -tags "${BUILD_TAGS}" \
  -ldflags "-s -w -X github.com/cogitatorai/cogitator/server/internal/version.Version=${VERSION}" \
  -o cogitator ./cmd/cogitator/

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=builder /build/cogitator /usr/local/bin/cogitator
COPY --from=dashboard /build/dist /usr/local/share/cogitator/dashboard

# Run as an unprivileged user. The workspace volume (/data) holds the SQLite
# DB and secrets.yaml, so it must be writable by this user.
RUN addgroup -g 1000 cogitator && \
  adduser -D -u 1000 -G cogitator cogitator && \
  mkdir -p /data && \
  chown -R cogitator:cogitator /data

EXPOSE 8484
ENV COGITATOR_WORKSPACE_PATH=/data
ENV COGITATOR_DASHBOARD_DIR=/usr/local/share/cogitator/dashboard

USER cogitator

HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
  CMD wget -q -O /dev/null "http://localhost:${COGITATOR_SERVER_PORT:-8484}/api/health" || exit 1

ENTRYPOINT ["cogitator"]
