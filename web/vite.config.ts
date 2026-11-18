import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

// Dev: same-origin /api like nginx in Docker, so the refresh cookie and SSE work unchanged.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    proxy: {
      '/api': {
        target: process.env.SIGNALING_URL ?? 'http://localhost:8081',
        rewrite: (p) => p.replace(/^\/api/, ''),
      },
    },
  },
})
