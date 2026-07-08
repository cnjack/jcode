import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { fileURLToPath, URL } from 'node:url'

// mirroring web/vite.config.ts: builds into ../internal/web/dist (consumed by the
// Go embed.FS and the Tauri shell), and proxies /api → the Go server on :8080 in dev.
export default defineConfig({
  plugins: [tailwindcss(), react()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  build: {
    // NOTE: still points at the Vue dist during the migration so both builds can
    // coexist. The switch-over (Makefile build-web + this outDir) happens in the
    // final phase once the React app is feature-complete.
    outDir: '../internal/web/dist-react',
    emptyOutDir: true,
  },
  server: {
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:8080',
        ws: true,
      },
    },
  },
})
