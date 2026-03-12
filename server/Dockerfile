FROM golang:1.25-alpine AS builder

ARG BUILD_TAGS=""

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -tags "${BUILD_TAGS}" -o cogitator ./cmd/cogitator/

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=builder /build/cogitator /usr/local/bin/cogitator

EXPOSE 8484
VOLUME /data
ENV COGITATOR_WORKSPACE_PATH=/data

ENTRYPOINT ["cogitator"]
