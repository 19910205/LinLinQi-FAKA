import { defineConfig, loadEnv } from "vite";
import vue from "@vitejs/plugin-vue";
import { fileURLToPath } from "url";
import { dirname, resolve } from "path";

// Root of the repository (contains the single source-of-truth .env).
const rootDir = resolve(dirname(fileURLToPath(import.meta.url)), "..");

// https://vite.dev/config/
export default defineConfig(({ mode }) => {
  // Read the storefront port and the dev API base URL from the root .env.
  const rootEnv = loadEnv(mode, rootDir, "");
  const define: Record<string, string> = {};
  if (!process.env.VITE_API_BASE_URL && rootEnv.PUBLIC_USER_API_URL) {
    define["import.meta.env.VITE_API_BASE_URL"] = JSON.stringify(
      rootEnv.PUBLIC_USER_API_URL
    );
  }
  return {
    plugins: [vue()],
    server: { port: Number(rootEnv.USER_PUBLISHED_PORT || "8080") },
    define,
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
  };
});
