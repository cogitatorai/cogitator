FROM golang:1.25-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /orchestrator ./cmd/orchestrator

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=builder /orchestrator /usr/local/bin/orchestrator
EXPOSE 8485
ENTRYPOINT ["orchestrator"]
