import { defineConfig, loadEnv } from "vite";
import vue from "@vitejs/plugin-vue";
import { fileURLToPath } from "url";
import { dirname, resolve } from "path";

// Root of the repository (contains the single source-of-truth .env).
const rootDir = resolve(dirname(fileURLToPath(import.meta.url)), "..");

// https://vite.dev/config/
export default defineConfig(({ mode }) => {
  // Read the admin port and the dev API base URL from the root .env.
  const rootEnv = loadEnv(mode, rootDir, "");
  const define: Record<string, string> = {};
  if (!process.env.VITE_API_BASE_URL && rootEnv.PUBLIC_ADMIN_API_URL) {
    define["import.meta.env.VITE_API_BASE_URL"] = JSON.stringify(
      rootEnv.PUBLIC_ADMIN_API_URL
    );
  }
  return {
    plugins: [vue()],
    server: { port: Number(rootEnv.ADMIN_PUBLISHED_PORT || "8082") },
    define,
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
  };
});
