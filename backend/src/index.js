/**
 * index.js
 * Application entry point.
 *
 * tracing.js MUST be the first import. It registers the OpenTelemetry SDK
 * and monkey-patches HTTP, Express, and Mongoose before any of those
 * modules are loaded.
 */

// --- OpenTelemetry must come first ---
import './tracing.js';

import 'dotenv/config';
import express from 'express';
import { connect, isConnected, disconnect } from './db.js';
import itemsRouter from './routes/items.js';
import { logger } from './logger.js';

const PORT = process.env.PORT ?? 3000;
const MONGO_URI = process.env.MONGO_URI;

if (!MONGO_URI) {
  logger.error('MONGO_URI environment variable is not set. Exiting.');
  process.exit(1);
}

const app = express();

// Parse JSON request bodies. Limit size to prevent large-payload attacks.
app.use(express.json({ limit: '100kb' }));

// ---------------------------------------------------------------------------
// Request logging middleware
// Logs every non-health request with method, path, status, and duration.
// Because this runs after the OTel Express instrumentation has set up spans,
// the trace_id and span_id are already in scope when logger.info is called.
// ---------------------------------------------------------------------------
app.use((req, res, next) => {
  // Skip health checks to avoid log noise from Kubernetes probes.
  if (req.path === '/health') return next();

  const start = Date.now();
  res.on('finish', () => {
    logger.info('http request', {
      method: req.method,
      path: req.path,
      status: res.statusCode,
      duration_ms: Date.now() - start,
    });
  });
  next();
});

// ---------------------------------------------------------------------------
// Routes
// ---------------------------------------------------------------------------

/**
 * GET /health
 * Returns HTTP 200 when the server is up and the database is connected.
 * Returns HTTP 503 when the database is unreachable.
 */
app.get('/health', (req, res) => {
  const dbUp = isConnected();
  const status = dbUp ? 'ok' : 'degraded';
  const httpCode = dbUp ? 200 : 503;

  res.status(httpCode).json({
    status,
    db: dbUp ? 'connected' : 'disconnected',
    timestamp: new Date().toISOString(),
  });
});

app.use('/api/items', itemsRouter);

// ---------------------------------------------------------------------------
// Centralized error handler
// ---------------------------------------------------------------------------
// eslint-disable-next-line no-unused-vars
app.use((err, req, res, next) => {
  const status = err.status ?? 500;

  if (status >= 500) {
    logger.error('unhandled error', {
      method: req.method,
      url: req.originalUrl,
      status,
      message: err.message,
      stack: err.stack,
    });
  } else {
    logger.warn('client error', {
      method: req.method,
      url: req.originalUrl,
      status,
      message: err.message,
    });
  }

  res.status(status).json({
    error: {
      message: err.message,
      ...(process.env.NODE_ENV !== 'production' && { stack: err.stack }),
    },
  });
});

// ---------------------------------------------------------------------------
// Startup
// ---------------------------------------------------------------------------
let server;

async function start() {
  try {
    logger.info('connecting to MongoDB');
    await connect(MONGO_URI);
    logger.info('MongoDB connected');

    server = app.listen(PORT, () => {
      logger.info('HTTP server listening', { port: PORT });
    });
  } catch (err) {
    logger.error('failed to start', { error: err.message });
    process.exit(1);
  }
}

// ---------------------------------------------------------------------------
// Graceful shutdown
// ---------------------------------------------------------------------------
async function shutdown(signal) {
  logger.info('shutting down', { signal });

  if (server) {
    server.close(async () => {
      logger.info('HTTP server closed');
      try {
        await disconnect();
        logger.info('MongoDB connection closed');
      } catch (err) {
        logger.error('error closing MongoDB', { error: err.message });
      }
      process.exit(0);
    });

    setTimeout(() => {
      logger.error('forced exit after shutdown timeout');
      process.exit(1);
    }, 10_000).unref();
  } else {
    process.exit(0);
  }
}

process.on('SIGTERM', () => shutdown('SIGTERM'));
process.on('SIGINT',  () => shutdown('SIGINT'));

start();
