import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

// Silence noisy EPIPE/ECONNRESET errors from the WS proxy.
// Vite logs these internally before our error handlers run,
// so we intercept console.error for proxy-related messages.
const origConsoleError = console.error;
console.error = (...args: unknown[]) => {
  const msg = typeof args[0] === 'string' ? args[0] : '';
  if (msg.includes('[vite] ws proxy')) return;
  origConsoleError.apply(console, args);
};

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    proxy: {
      '/api/ollama/pull': {
        target: 'http://127.0.0.1:8484',
        changeOrigin: true,
        headers: { 'Accept': 'text/event-stream' },
        configure: (proxy) => {
          proxy.on('error', () => {});
          proxy.on('proxyRes', (proxyRes) => {
            // Disable buffering for SSE responses.
            proxyRes.headers['x-accel-buffering'] = 'no';
            proxyRes.headers['cache-control'] = 'no-cache';
          });
        },
      },
      '/api': {
        target: 'http://127.0.0.1:8484',
        changeOrigin: true,
        configure: (proxy) => {
          proxy.on('error', () => {});
        },
      },
      '/ws': {
        target: 'ws://127.0.0.1:8484',
        ws: true,
        configure: (proxy) => {
          proxy.on('error', () => {});
          proxy.on('proxyReqWs', (_proxyReq, _req, socket) => {
            socket.on('error', () => {});
          });
        },
      },
    },
  },
})
