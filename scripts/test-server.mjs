import { spawn, execFileSync } from "node:child_process";
import { mkdirSync, rmSync } from "node:fs";
import { join, basename } from "node:path";

import { startMetaTubeFixture } from "./metatube-fixture.mjs";
import { startTVDBFixture } from "./tvdb-fixture.mjs";
import { startTMDBFixture } from "./tmdb-fixture.mjs";

const root = process.env.REELNEST_TEST_ROOT;
if (!root || !basename(root).startsWith("reelnest-browser-"))
  throw new Error("Isolated test root required");
mkdirSync(root, { recursive: true });
mkdirSync(join(root, "media"), { recursive: true });
const binary = join(root, "reelnest");
execFileSync("go", ["build", "-o", binary, "./cmd/reelnest"], {
  stdio: "inherit",
});
const metadataBinary = join(root, "reelnest-metadata");
execFileSync("go", ["build", "-o", metadataBinary, "./tests/server"], {
  stdio: "inherit",
});
const fixture = await startTMDBFixture();
const tvdbFixture = await startTVDBFixture();
const metaTubeFixture = await startMetaTubeFixture();
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
    METATUBE_URL: "",
    METATUBE_TOKEN: "",
    METATUBE_TOKEN_FILE: "",
    TVDB_API_KEY: "",
    TVDB_API_KEY_FILE: join(root, "intentionally-missing-tvdb-key"),
    TVDB_PIN: "",
    TVDB_PIN_FILE: "",
    TMDB_READ_ACCESS_TOKEN: "",
    // An unreadable optional token file must not prevent the real app starting.
    TMDB_READ_ACCESS_TOKEN_FILE: join(root, "intentionally-missing-tmdb-token"),
  },
  stdio: "inherit",
});
let stopping = false;
function stop() {
  if (stopping) return;
  stopping = true;
  child.kill("SIGTERM");
  fixture.close();
  tvdbFixture.close();
  metaTubeFixture.close();
}
for (const signal of ["SIGINT", "SIGTERM"]) process.on(signal, stop);
child.on("error", (error) => {
  console.error(error);
  stop();
  process.exitCode = 1;
});
child.on("exit", (code) => {
  stop();
  rmSync(root, { recursive: true, force: true });
  process.exit(code ?? 0);
});
