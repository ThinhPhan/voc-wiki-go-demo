# syntax=docker/dockerfile:1
FROM golang:1.22-alpine AS builder

# Install gcc for cgo (required by go-sqlite3)
RUN apk add --no-cache gcc musl-dev

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY main.go ./
RUN CGO_ENABLED=1 GOOS=linux go build -ldflags="-s -w" -o wiki .

# ── Runtime ──────────────────────────────────────────────────────────────────
FROM alpine:3.19

RUN apk add --no-cache ca-certificates sqlite-libs

WORKDIR /app
COPY --from=builder /app/wiki .

# Storage volume for SQLite database
VOLUME ["/storage"]

EXPOSE 8080
ENTRYPOINT ["/app/wiki"]
