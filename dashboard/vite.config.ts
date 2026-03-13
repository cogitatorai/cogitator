import path from 'path'
import { defineConfig, loadEnv } from 'vite'
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

export default defineConfig(({ mode }) => {
  // Load COGITATOR_* vars from the parent directory's .env file.
  const env = loadEnv(mode, path.resolve(__dirname, '..'), 'COGITATOR_')
  const port = env.COGITATOR_SERVER_PORT || '8484'
  const backend = `http://localhost:${port}`

  return {
    plugins: [react(), tailwindcss()],
    server: {
      proxy: {
        '/api/ollama/pull': {
          target: backend,
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
          target: backend,
          changeOrigin: true,
          configure: (proxy) => {
            proxy.on('error', () => {});
          },
        },
        '/ws': {
          target: `ws://localhost:${port}`,
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
  }
})
