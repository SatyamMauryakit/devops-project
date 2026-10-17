# =============================================================================
# Stage 1: build
# =============================================================================
FROM golang:1.22.4-alpine3.19 AS build

WORKDIR /app

# Copy module files first for layer caching.
COPY go.mod go.sum ./
RUN go mod download

# Copy all source files and build a statically linked binary.
COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -o agent .

# =============================================================================
# Stage 2: runtime
# gcr.io/distroless/static:nonroot has CA certs, no shell, runs as uid 65532.
# =============================================================================
FROM gcr.io/distroless/static:nonroot AS runtime

COPY --from=build /app/agent /agent

# distroless/static:nonroot sets USER 65532:65532 automatically.

EXPOSE 8081

HEALTHCHECK --interval=15s --timeout=5s --start-period=15s --retries=3 \
  CMD ["/agent", "-health-check-only"]

ENTRYPOINT ["/agent"]
