# Build stage
FROM golang:1.25-alpine AS builder
WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY cmd/     cmd/
COPY internal/ internal/
COPY adapters/ adapters/

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o akb48 ./cmd/akb48/

# Runtime stage
FROM alpine:3.20

RUN apk --no-cache add ca-certificates tzdata nodejs npm && \
    npm install -g @anthropic-ai/claude-code @modelcontextprotocol/server-memory && \
    addgroup -S akb48 && adduser -S akb48 -G akb48

WORKDIR /app

COPY --from=builder /build/akb48 .
COPY config.toml    .
COPY identity/      identity/
COPY skills/        skills/

RUN mkdir -p sessions logs brain && chown -R akb48:akb48 /app

USER akb48

ENTRYPOINT ["./akb48"]
CMD ["--config", "config.toml"]
