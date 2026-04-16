# ── Stage 1: Build ───────────────────────────────────────────────────────────
# Use the official Go Alpine image for a minimal build environment.
FROM golang:alpine AS builder

WORKDIR /app

# Download dependencies first — cached layer, only re-runs on go.mod changes.
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build the single LOSU binary.
# CGO_ENABLED=0 produces a fully static binary — no libc dependency.
# This means it runs on any Linux image including scratch/alpine.
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /losu cmd/logsum/main.go

# ── Stage 2: Runtime ──────────────────────────────────────────────────────────
# Minimal Alpine runtime — ca-certificates needed for HTTPS (ntfy, Ollama).
FROM alpine:latest
RUN apk --no-cache add ca-certificates

WORKDIR /app

# Copy only the binary — no .env, no generators, no source.
# All configuration is injected via environment variables at runtime.
COPY --from=builder /losu .

# Expose web dashboard port.
# Only active when running with --ui=web or --ui=both.
EXPOSE 8080

# Default: run with both TUI and web dashboard.
# Override with: docker run ... losu --ui=tui
ENTRYPOINT ["./losu", "--ui=both"]