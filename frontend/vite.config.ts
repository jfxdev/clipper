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
  server: {
    // changeOrigin stays off (the default): keeping the Host header as
    // localhost:5173 makes the backend's RequireSameOrigin check see the
    // request as same-origin instead of rejecting it as cross-site.
    proxy: {
      // The string-shorthand form (`"/api": "http://..."`) sets
      // changeOrigin: true implicitly, which rewrites the Host header to
      // the target and makes the backend's RequireSameOrigin check see a
      // cross-site request and reject it. The object form is needed to
      // pin changeOrigin: false and keep Host as localhost:5173.
      "/api": {
        target: "http://localhost:8080",
        changeOrigin: false,
      },
    },
  },
})
