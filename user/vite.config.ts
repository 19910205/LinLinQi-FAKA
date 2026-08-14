import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";

// https://vite.dev/config/
export default defineConfig({
  plugins: [vue()],
  server: { port: 5173 },
  build: {
    rollupOptions: {
      output: {
        // Avoid false positives from browser-extension filter lists that use
        // business words in lazy-loaded module filenames.
        entryFileNames: "assets/e-[hash].js",
        chunkFileNames: "assets/c-[hash].js",
        assetFileNames: "assets/a-[hash][extname]",
      },
    },
  },
});
