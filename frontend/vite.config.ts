import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// En desarrollo, /api se reenvía a la API local; en producción la sirve el
// mismo binario de Go, así que el navegador usa un único origen.
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: { '/api': process.env.VITE_API_TARGET ?? 'http://localhost:8080' },
  },
  build: { outDir: 'dist', sourcemap: false },
})
