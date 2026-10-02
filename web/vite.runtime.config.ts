// Isolated copy of the same production frontend for temporary runtime fixtures.
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { resolve } from "node:path";

export default defineConfig({
  root: "web",
  plugins: [react()],
  build: {
    outDir: "../.reelnest-test/runtime-web",
    emptyOutDir: true,
    rollupOptions: { input: resolve("web/index.html") },
  },
});
