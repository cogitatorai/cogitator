FROM node:20-alpine AS dashboard
WORKDIR /build
COPY dashboard/package.json dashboard/package-lock.json ./
RUN npm ci
COPY dashboard/ .
RUN npm run build

FROM golang:1.25-alpine AS builder
ARG BUILD_TAGS=""
WORKDIR /build
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ .
RUN CGO_ENABLED=0 go build -tags "${BUILD_TAGS}" -o cogitator ./cmd/cogitator/

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=builder /build/cogitator /usr/local/bin/cogitator
COPY --from=dashboard /build/dist /usr/local/share/cogitator/dashboard

EXPOSE 8484
VOLUME /data
ENV COGITATOR_WORKSPACE_PATH=/data
ENV COGITATOR_DASHBOARD_DIR=/usr/local/share/cogitator/dashboard

ENTRYPOINT ["cogitator"]
