/**
 * tracing.js
 * OpenTelemetry SDK initialisation.
 *
 * MUST be the first import in index.js.
 * Auto-instrumentation works by monkey-patching modules at require/import time.
 * Any module imported before this file runs will NOT be instrumented.
 *
 * What this file sets up:
 *  - Resource attributes: service.name, service.version, deployment.environment
 *  - Trace exporter: OTLP over gRPC to the OTel Collector
 *  - Metrics exporter: OTLP over gRPC to the OTel Collector
 *  - Auto-instrumentation: HTTP, Express, Mongoose (via @opentelemetry/auto-instrumentations-node)
 *
 * Environment variables (all optional; safe defaults for local dev):
 *  OTEL_EXPORTER_OTLP_ENDPOINT  - gRPC endpoint of the collector  (default: http://localhost:4317)
 *  OTEL_SERVICE_NAME            - overrides the service name        (default: devops-sentinel-backend)
 *  NODE_ENV                     - sets deployment.environment        (default: development)
 */

import { NodeSDK } from '@opentelemetry/sdk-node';
import { getNodeAutoInstrumentations } from '@opentelemetry/auto-instrumentations-node';
import { OTLPTraceExporter } from '@opentelemetry/exporter-trace-otlp-grpc';
import { OTLPMetricExporter } from '@opentelemetry/exporter-metrics-otlp-grpc';
import { PeriodicExportingMetricReader } from '@opentelemetry/sdk-metrics';
import { Resource } from '@opentelemetry/resources';
import { SEMRESATTRS_SERVICE_NAME, SEMRESATTRS_SERVICE_VERSION, SEMRESATTRS_DEPLOYMENT_ENVIRONMENT } from '@opentelemetry/semantic-conventions';

const collectorEndpoint =
  process.env.OTEL_EXPORTER_OTLP_ENDPOINT ?? 'http://localhost:4317';

const resource = new Resource({
  [SEMRESATTRS_SERVICE_NAME]:
    process.env.OTEL_SERVICE_NAME ?? 'devops-sentinel-backend',
  [SEMRESATTRS_SERVICE_VERSION]: '1.0.0',
  [SEMRESATTRS_DEPLOYMENT_ENVIRONMENT]: process.env.NODE_ENV ?? 'development',
});

const traceExporter = new OTLPTraceExporter({
  url: collectorEndpoint,
});

const metricExporter = new OTLPMetricExporter({
  url: collectorEndpoint,
});

const sdk = new NodeSDK({
  resource,
  traceExporter,
  metricReader: new PeriodicExportingMetricReader({
    exporter: metricExporter,
    // Export metrics every 15 seconds.
    exportIntervalMillis: 15_000,
  }),
  instrumentations: [
    getNodeAutoInstrumentations({
      // HTTP instrumentation: record inbound and outbound HTTP spans.
      '@opentelemetry/instrumentation-http': {
        enabled: true,
        // Do not create spans for health check calls to avoid noise.
        ignoreIncomingRequestHook: (req) => req.url === '/health',
      },
      // Express instrumentation: record route-level spans.
      '@opentelemetry/instrumentation-express': { enabled: true },
      // Mongoose instrumentation: record database query spans.
      '@opentelemetry/instrumentation-mongoose': { enabled: true },
      // Disable fs and dns instrumentation - they produce many low-value spans.
      '@opentelemetry/instrumentation-fs': { enabled: false },
      '@opentelemetry/instrumentation-dns': { enabled: false },
    }),
  ],
});

// Start the SDK synchronously before any other imports.
// SDK errors are logged but do not crash the application so that
// a collector outage does not take down the backend.
try {
  sdk.start();
  console.log(
    JSON.stringify({
      level: 'info',
      service: { name: process.env.OTEL_SERVICE_NAME ?? 'devops-sentinel-backend' },
      message: 'OpenTelemetry SDK started',
      collector: collectorEndpoint,
      timestamp: new Date().toISOString(),
    })
  );
} catch (err) {
  console.error(
    JSON.stringify({
      level: 'error',
      message: 'OpenTelemetry SDK failed to start',
      error: err.message,
      timestamp: new Date().toISOString(),
    })
  );
}

// Flush all pending spans and metrics on process exit.
process.on('SIGTERM', async () => {
  try {
    await sdk.shutdown();
  } catch (err) {
    // Shutdown errors are non-fatal; the process is exiting anyway.
  }
});

// Export the api module so route handlers can read the active span.
export { default as otelApi } from '@opentelemetry/api';
