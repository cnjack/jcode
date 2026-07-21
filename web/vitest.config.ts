import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  test: {
    environment: 'jsdom',
    environmentOptions: {
      jsdom: {
        // Non-opaque origin so window.localStorage exists (locale persistence).
        url: 'http://localhost/',
      },
    },
    include: ['src/**/*.test.{ts,tsx}'],
  },
})
