# =============================================================================
# Stage 1: build
# Install all dependencies (including devDependencies) and run vite build.
# =============================================================================
FROM node:20.14.0-alpine3.19 AS build

WORKDIR /app

# Copy manifests first for layer caching.
COPY package.json package-lock.json ./

RUN npm ci --ignore-scripts

# Copy application source and build.
COPY index.html vite.config.js ./
COPY src ./src

RUN npm run build
# Output is in /app/dist

# =============================================================================
# Stage 2: runtime
# Minimal nginx image that serves the built assets.
# Runs as the nginx non-root user (uid 101 in this image).
# =============================================================================
FROM nginx:1.27.0-alpine3.19 AS runtime

# Remove the default nginx configuration.
RUN rm /etc/nginx/conf.d/default.conf

# Copy our custom config.
COPY nginx/nginx.conf /etc/nginx/nginx.conf

# Copy the built React app into the nginx web root.
COPY --from=build /app/dist /usr/share/nginx/html

# nginx:alpine already drops privileges to the "nginx" user (uid 101) by default
# because the worker_processes directive in our config does not set a user line.
# The master process still starts as root to bind port 80, then drops privileges.
# For a fully non-root setup a port >1024 would be needed; port 80 is standard
# for a container that sits behind a load balancer or compose network.

EXPOSE 80

# Health check: a simple curl to the root path confirms nginx is serving.
HEALTHCHECK --interval=15s --timeout=5s --start-period=20s --retries=3 \
  CMD wget --quiet --tries=1 --spider http://localhost:80/ || exit 1

CMD ["nginx", "-g", "daemon off;"]
