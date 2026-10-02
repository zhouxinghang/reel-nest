import { execFileSync } from "node:child_process";
import { createServer } from "node:http";
import { lstatSync, readdirSync, realpathSync, rmSync } from "node:fs";
import { join, basename, dirname, isAbsolute } from "node:path";
import { tmpdir } from "node:os";
import { startMetaTubeFixture } from "./metatube-fixture.mjs";
import { startTVDBFixture } from "./tvdb-fixture.mjs";
import { startTMDBFixture } from "./tmdb-fixture.mjs";

const root = process.env.REELNEST_TEST_ROOT;
if (
  !root ||
  !isAbsolute(root) ||
  !basename(root).startsWith("reelnest-browser-") ||
  lstatSync(root).isSymbolicLink() ||
  dirname(realpathSync(root)) !== realpathSync(tmpdir()) ||
  readdirSync(root).length !== 0
)
  throw new Error(
    "A fresh, empty, non-symlink test directory directly under the OS temp directory is required",
  );
for (const [name, source] of [
  ["reelnest-production", "./cmd/reelnest"],
  ["reelnest-runtime", "./tests/runtime-server"],
])
  execFileSync("go", ["build", "-o", join(root, name), source], {
    stdio: "inherit",
  });
const fixtures = [
  await startTMDBFixture(),
  await startTVDBFixture(),
  await startMetaTubeFixture(),
];
// Only fixture readiness, not an alternate application. Each spec starts the
// same current runtime with its own temporary database and media directory.
const health = createServer((req, res) => {
  res.writeHead(req.url === "/healthz" ? 200 : 404);
  res.end();
});
await new Promise((resolve, reject) => {
  health.once("error", reject);
  health.listen(18090, "127.0.0.1", resolve);
});
let stopping = false;
async function stop() {
  if (stopping) return;
  stopping = true;
  await Promise.all(
    [health, ...fixtures].map(
      (server) => new Promise((resolve) => server.close(resolve)),
    ),
  );
  rmSync(root, { recursive: true, force: true });
}
for (const signal of ["SIGINT", "SIGTERM"])
  process.on(signal, () => void stop());
