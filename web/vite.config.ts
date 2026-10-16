// Copyright (C) 2026 White Ravens. AGPL-3.0-only with an additional term; see LICENSE and NOTICE.

import { defineConfig, loadEnv } from 'vite';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';

// In development the client runs on its own port and the API is proxied, so
// the browser sees one origin and the session cookie works as in production.
// Set DARKNODE_API in the environment to point the proxy at another address.
export default defineConfig(({ mode }) => {
  const api = loadEnv(mode, '.', 'DARKNODE_').DARKNODE_API ?? 'http://127.0.0.1:8099';
  return {
    plugins: [react(), tailwindcss()],
    build: { outDir: 'dist', sourcemap: false },
    server: {
      port: 5173,
      proxy: {
        '/api': { target: api, changeOrigin: false },
        '/healthz': { target: api },
      },
    },
  };
});
