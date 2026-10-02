// Temporary old-UI build used ONLY by historical regression harnesses (issue 06).
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
export default defineConfig({
  root: "web",
  plugins: [
    react(),
    {
      name: "isolated-legacy-entry",
      transformIndexHtml: {
        order: "pre",
        handler: (html) => html.replace("/src/app/main.tsx", "/src/main.tsx"),
      },
    },
  ],
  build: { outDir: "../.reelnest-test/legacy-web", emptyOutDir: true },
});
