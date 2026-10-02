import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
export default defineConfig({
  plugins: [react()],
  test: {
    environment: "jsdom",
    include: ["web/src/**/*.test.{ts,tsx}"],
    restoreMocks: true,
    setupFiles: ["web/test-setup.ts"],
  },
});
