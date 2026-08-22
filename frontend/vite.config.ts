import path from "node:path"
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
  build: {
    // The Content-Security-Policy served by the backend is script-src
    // 'self' with no inline allowance. Vite's module-preload polyfill is
    // injected as an inline <script>, which would be blocked outright —
    // turning it off keeps the policy free of hash or nonce exceptions.
    modulePreload: { polyfill: false },
    // No source maps in the production bundle: they would republish the
    // original crypto sources to anyone who opens devtools, for no benefit
    // to a user.
    sourcemap: false,
  },
  resolve: {
    alias: {
      "@": path.resolve(import.meta.dirname, "./src"),
    },
  },
})
