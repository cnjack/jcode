import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

const root = path.dirname(fileURLToPath(import.meta.url))

// jcode-ui is linked via file: and ships its own nested react (devDependency of
// the package). Without dedupe/alias, Vite serves two React copies →
// "Invalid hook call" / useState of null in the browser.
const reactPath = path.resolve(root, 'node_modules/react')
const reactDomPath = path.resolve(root, 'node_modules/react-dom')

export default defineConfig({
  plugins: [react()],
  resolve: {
    dedupe: ['react', 'react-dom', 'react/jsx-runtime', 'react/jsx-dev-runtime'],
    alias: {
      react: reactPath,
      'react-dom': reactDomPath,
      'react/jsx-runtime': path.resolve(reactPath, 'jsx-runtime.js'),
      'react/jsx-dev-runtime': path.resolve(reactPath, 'jsx-dev-runtime.js'),
    },
  },
  optimizeDeps: {
    include: ['react', 'react-dom', 'react/jsx-runtime', 'jcode-ui', 'jcode-ui-core'],
    // Force rebundle when local packages change
    exclude: [],
  },
  server: {
    // Keep off 5173 — that port is reserved for the product web UI
    // (web/vite.config.ts + desktop/src-tauri/tauri.conf.json devUrl).
    // Running `pnpm dev` in site on 5173 makes `make desktop-dev` load the
    // marketing site instead of jcode.
    port: 5199,
    strictPort: false,
    fs: {
      // allow importing from monorepo packages/
      allow: [root, path.resolve(root, '..')],
    },
  },
  build: {
    chunkSizeWarningLimit: 1200,
    commonjsOptions: {
      include: [/node_modules/],
    },
  },
})
