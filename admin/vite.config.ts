import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";

// https://vite.dev/config/
export default defineConfig({
  plugins: [vue()],
  server: { port: 5174 },
  build: {
    rollupOptions: {
      output: {
        // Semantic chunk names such as DashboardView/AnalyticsView are
        // incorrectly blocked by some privacy and wallet extensions. Content
        // hashes keep cache busting while exposing no filter-list keywords.
        entryFileNames: "assets/e-[hash].js",
        chunkFileNames: "assets/c-[hash].js",
        assetFileNames: "assets/a-[hash][extname]",
      },
    },
  },
});
