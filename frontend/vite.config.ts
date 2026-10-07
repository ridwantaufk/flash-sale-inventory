import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// Dev talks to the backend on the host. In the container nginx does the same
// job, so the browser only ever sees /api.
const backend = process.env.VITE_BACKEND_URL ?? 'http://localhost:8080'

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    host: true,
    proxy: {
      '/api': backend,
      '/healthz': backend,
    },
  },
})
