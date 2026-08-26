import { defineConfig } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'

// base './' is required: Viam Applications serves the bundle from a
// sub-path, so asset URLs must be relative.
export default defineConfig({
  plugins: [svelte()],
  base: './',
  build: {
    outDir: './dist',
    emptyOutDir: true,
  },
})
