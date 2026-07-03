import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import { fileURLToPath } from 'node:url'
import { dirname, resolve } from 'node:path'

const here = dirname(fileURLToPath(import.meta.url))

export default defineConfig({
  plugins: [react()],
  server: {
    fs: {
      // docs markdown is imported straight from the repo's docs/ directory
      allow: [resolve(here, '..')],
    },
  },
  build: {
    chunkSizeWarningLimit: 1200,
  },
})
