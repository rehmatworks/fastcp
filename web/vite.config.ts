/// <reference types="vitest/config" />
import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';

// The panel serves the UI with a strict Content-Security-Policy, so the
// build must not rely on inline scripts or style attributes.
export default defineConfig({
  plugins: [svelte()],
  build: {
    outDir: '../cmd/fastcp/ui/dist',
    emptyOutDir: true,
    assetsDir: 'assets',
    target: 'es2022',
    sourcemap: false,
    chunkSizeWarningLimit: 600,
  },
  // `make dev` runs the panel on plain HTTP at this address.
  server: {
    port: 5173,
    proxy: {
      '/api': { target: 'http://127.0.0.1:2050' },
      '/phpmyadmin': { target: 'http://127.0.0.1:2050' },
    },
  },
  test: {
    include: ['src/**/*.test.ts'],
    environment: 'node',
  },
});
