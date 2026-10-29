import react from '@vitejs/plugin-react';
import { defineConfig } from 'vite';

// The gateway's /admin/* routes deliberately have no CORS headers (see
// internal/httpserver — admin and /v1 routes get no CORS, only
// /public/metrics does). So in dev, proxy /admin and /public through Vite's
// own server, keeping the browser on a single origin (localhost:5173) exactly
// as it will be in production when this build is reverse-proxied alongside
// the gateway. Override the target with GATEWAY_URL if the gateway isn't on
// the default docker-compose port.
const target = process.env.GATEWAY_URL ?? 'http://localhost:8080';

export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/admin': { target, changeOrigin: true },
      '/public': { target, changeOrigin: true },
    },
  },
});
