// Staged frontend verification only. The CLI's embedded index remains unchanged
// until all issue 05 interactions have replacements and end-to-end coverage.
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { resolve } from "node:path";

export default defineConfig({
  root: "web",
  plugins: [react()],
  build: {
    outDir: "../.reelnest-test/runtime-web",
    emptyOutDir: true,
    rollupOptions: { input: resolve("web/runtime.html") },
  },
});
