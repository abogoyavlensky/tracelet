import { writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { defineConfig, type Plugin } from 'vitest/config'

// The build lands in internal/web/dist, which the Go binary embeds. Vite
// names every file under assets/ with a content hash, so the server serves
// that directory with a one-year immutable cache and index.html uncached.
const outDir = resolve(import.meta.dirname, '../internal/web/dist')

// Vite empties outDir before writing, which would delete the committed
// .gitkeep that lets `go build` embed the directory before a frontend build.
function keepDist(): Plugin {
  return {
    name: 'keep-dist',
    closeBundle() {
      writeFileSync(resolve(outDir, '.gitkeep'), '')
    },
  }
}

export default defineConfig({
  plugins: [react(), tailwindcss(), keepDist()],
  build: {
    outDir,
    emptyOutDir: true,
  },
  server: {
    proxy: {
      '/api': 'http://localhost:8080',
    },
  },
  test: {
    environment: 'node',
    include: ['src/**/*.test.ts'],
  },
})
