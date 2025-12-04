import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import vueDevTools from 'vite-plugin-vue-devtools'
import { viteSingleFile } from 'vite-plugin-singlefile'
import favicons from '@peterek/vite-plugin-favicons'

// https://vite.dev/config/
export default defineConfig({
  plugins: [
    vue(),
    vueDevTools(),
    viteSingleFile(),
    favicons('src/assets/favicon.png', {
      icons: {
        favicons: ["favicon.ico"],
        android: false,
        appleIcon: false,
        appleStartup: false,
        windows: false,
        yandex: false,
      },
    }),
  ],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url))
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    copyPublicDir: false,
  },
})
