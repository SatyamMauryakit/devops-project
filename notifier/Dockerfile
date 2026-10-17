# =============================================================================
# Stage 1: build
# =============================================================================
FROM golang:1.22.4-alpine3.19 AS build

WORKDIR /app

# Copy module files first for layer caching.
COPY go.mod ./
RUN go mod download

# Copy source and build a statically linked binary.
COPY main.go ./

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -o notifier .

# =============================================================================
# Stage 2: runtime
# gcr.io/distroless/static is minimal (no shell), has CA certs, and supports
# running as a non-root user out of the box. The "nonroot" tag pins to uid 65532.
# =============================================================================
FROM gcr.io/distroless/static:nonroot AS runtime

COPY --from=build /app/notifier /notifier

# distroless/static:nonroot already sets USER 65532:65532.
# No USER instruction needed.

EXPOSE 8080

# Distroless does not have a shell, so HEALTHCHECK exec form is used directly.
HEALTHCHECK --interval=15s --timeout=5s --start-period=10s --retries=3 \
  CMD ["/notifier", "-health-check-only"]

ENTRYPOINT ["/notifier"]
