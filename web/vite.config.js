import { defineConfig } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'

export default defineConfig({
  plugins: [svelte()],
  server: {
    port: 5173,
    proxy: {
      '/ws': {
        target: 'http://localhost:4000',
        ws: true
      },
      '/api': 'http://localhost:4000'
    }
  }
})
