import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// In development, proxy /api to a running backend (for example through an SSH tunnel to the AP).
const backend = process.env.C460_BACKEND ?? 'http://127.0.0.1:18099'

export default defineConfig({
  base: '/',
  plugins: [react()],
  resolve: { alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) } },
  server: { port: 5175, host: true, proxy: { '/api': { target: backend, changeOrigin: false } } },
  build: { outDir: 'dist', sourcemap: false, chunkSizeWarningLimit: 900 },
})
