/**
 * logger.js
 * Structured JSON logger.
 *
 * Every log line is a single JSON object written to stdout.
 * The fields service.name, trace_id, and span_id are injected automatically
 * from the active OpenTelemetry span so that Loki can correlate a log line
 * with its trace in Tempo.
 *
 * Usage:
 *   import { logger } from './logger.js';
 *   logger.info('item created', { item_id: item._id });
 *   logger.error('something failed', { error: err.message });
 */

import api from '@opentelemetry/api';

const SERVICE_NAME =
  process.env.OTEL_SERVICE_NAME ?? 'devops-sentinel-backend';

/**
 * Returns the trace_id and span_id of the currently active span,
 * or empty strings if no span is active.
 * These fields let Loki link a log line to its trace in Tempo.
 */
function getTraceContext() {
  const span = api.trace.getActiveSpan();
  if (!span) return { trace_id: '', span_id: '' };

  const ctx = span.spanContext();
  // OTel trace IDs are valid only when the trace flags indicate sampling.
  if (!api.isSpanContextValid(ctx)) return { trace_id: '', span_id: '' };

  return {
    trace_id: ctx.traceId,
    span_id: ctx.spanId,
  };
}

function write(level, message, extra = {}) {
  const line = {
    timestamp: new Date().toISOString(),
    level,
    'service.name': SERVICE_NAME,
    ...getTraceContext(),
    message,
    ...extra,
  };
  // Use process.stdout.write to avoid the newline that console.log adds
  // inconsistently on some Node.js versions.
  process.stdout.write(JSON.stringify(line) + '\n');
}

export const logger = {
  info:  (msg, extra) => write('info',  msg, extra),
  warn:  (msg, extra) => write('warn',  msg, extra),
  error: (msg, extra) => write('error', msg, extra),
  debug: (msg, extra) => write('debug', msg, extra),
};
