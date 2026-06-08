import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// Output is embedded into the gg binary (go:embed dashboard/dist). base './'
// keeps asset paths relative so they resolve under the embedded file server.
// Dev proxies /api to a locally running `gg serve` for hot-reload development.
export default defineConfig({
  plugins: [react()],
  base: './',
  build: { outDir: 'dist', emptyOutDir: true },
  server: {
    port: 5173,
    proxy: { '/api': 'http://127.0.0.1:7777' },
  },
})
