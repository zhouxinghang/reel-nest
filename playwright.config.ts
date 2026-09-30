import { defineConfig } from "@playwright/test";
import { existsSync, mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

process.env.REELNEST_TEST_ROOT ??= mkdtempSync(
  join(tmpdir(), "reelnest-browser-"),
);
const chrome = "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome";
export default defineConfig({
  testDir: "./tests",
  workers: 1,
  use: {
    baseURL: "http://127.0.0.1:18090",
    launchOptions: {
      executablePath:
        process.env.CHROME_BIN || (existsSync(chrome) ? chrome : undefined),
    },
    trace: "retain-on-failure",
  },
  webServer: {
    command: "node scripts/test-server.mjs",
    url: "http://127.0.0.1:18090/healthz",
    reuseExistingServer: false,
    timeout: 120000,
  },
});
