import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import path from 'node:path'

export default defineConfig({
  root: path.resolve(__dirname),
  plugins: [react()],
  resolve: {
    alias: {
      'jcode-ui-core': path.resolve(__dirname, '../../jcode-ui-core/src/index.ts'),
      'jcode-ui-core/primitives': path.resolve(__dirname, '../../jcode-ui-core/src/primitives/index.ts'),
      'jcode-ui-core/runtime': path.resolve(__dirname, '../../jcode-ui-core/src/runtime/index.ts'),
      'jcode-ui-core/adapters': path.resolve(__dirname, '../../jcode-ui-core/src/adapters/index.ts'),
    },
  },
  server: {
    port: 5199,
    strictPort: true,
  },
})
