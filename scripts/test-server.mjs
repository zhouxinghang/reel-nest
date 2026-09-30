import { spawn, execFileSync } from "node:child_process";
import { mkdirSync, rmSync } from "node:fs";
import { join, basename } from "node:path";

const root = process.env.REELNEST_TEST_ROOT;
if (!root || !basename(root).startsWith("reelnest-browser-"))
  throw new Error("Isolated test root required");
mkdirSync(root, { recursive: true });
mkdirSync(join(root, "media"), { recursive: true });
const binary = join(root, "reelnest");
execFileSync("go", ["build", "-o", binary, "./cmd/reelnest"], {
  stdio: "inherit",
});
const child = spawn(binary, [], {
  env: {
    ...process.env,
    DATA_DIR: join(root, "data"),
    MEDIA_ROOTS: join(root, "media"),
    LISTEN_ADDR: "127.0.0.1:18090",
    HOUSEHOLD_USERNAME: "family",
    HOUSEHOLD_PASSWORD: "browser-test-password",
    HOUSEHOLD_PASSWORD_FILE: "",
    COOKIE_SECURE: "false",
  },
  stdio: "inherit",
});
for (const signal of ["SIGINT", "SIGTERM"])
  process.on(signal, () => child.kill("SIGTERM"));
child.on("exit", (code) => {
  rmSync(root, { recursive: true, force: true });
  process.exit(code ?? 0);
});
