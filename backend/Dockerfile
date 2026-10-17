# =============================================================================
# Stage 1: deps
# Install only production dependencies using npm ci for a reproducible build.
# =============================================================================
FROM node:20.14.0-alpine3.19 AS deps

WORKDIR /app

# Copy manifests first so this layer is cached until they change.
COPY package.json package-lock.json ./

RUN npm ci --omit=dev --ignore-scripts

# =============================================================================
# Stage 2: runtime
# Slim final image. Runs as a non-root user.
# =============================================================================
FROM node:20.14.0-alpine3.19 AS runtime

# Add curl for the health check command.
RUN apk add --no-cache curl=8.8.0-r0

WORKDIR /app

# Create a non-root user and group.
RUN addgroup -S appgroup && adduser -S appuser -G appgroup

# Copy production node_modules from the deps stage.
COPY --from=deps /app/node_modules ./node_modules

# Copy application source.
COPY src ./src
COPY package.json ./

# Set ownership so appuser can read the files.
RUN chown -R appuser:appgroup /app

USER appuser

EXPOSE 3000

# Health check: call the /health endpoint.
# --fail makes curl return a non-zero exit code on HTTP 4xx/5xx.
HEALTHCHECK --interval=15s --timeout=5s --start-period=30s --retries=3 \
  CMD curl --fail http://localhost:3000/health || exit 1

CMD ["node", "src/index.js"]
