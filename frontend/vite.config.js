import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

/**
 * Vite configuration.
 *
 * During local development (npm run dev) Vite proxies /api requests to the
 * backend container so the frontend developer does not need to handle CORS.
 *
 * In production the nginx container handles this proxy; the vite dev server
 * proxy is only active when running outside Docker.
 */
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://localhost:3000',
        changeOrigin: true,
      },
    },
  },
});
