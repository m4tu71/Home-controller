import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import { readAppVersion } from './build/version'
import { resolve } from 'node:path'

const appVersion = readAppVersion(resolve(__dirname, '..'))

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],

  define: {
    __APP_VERSION__: JSON.stringify(`v${appVersion}`),
  },

  build: {
    rollupOptions: {
      output: {
        entryFileNames: `assets/[name]-v${appVersion}-[hash].js`,
        chunkFileNames: `assets/[name]-v${appVersion}-[hash].js`,
        assetFileNames: `assets/[name]-v${appVersion}-[hash][extname]`,
      },
    },
  },

  server: {
    host: "0.0.0.0",
    port: 5173,

    proxy: {
      "/api": {
        target: "http://localhost:3000",
        changeOrigin: true,
      },
    },
  },
});
