import { execFileSync } from "node:child_process";
import { readFile, readdir } from "node:fs/promises";
const packages = JSON.parse(
  execFileSync("go", ["list", "-json=ImportPath,Imports", "./internal/..."], {
    encoding: "utf8",
  })
    .replace(/}\s*{/g, "},{")
    .replace(/^/, "[")
    .replace(/$/, "]"),
);
const allowed = {
  app: [
    "catalog",
    "identity",
    "mediafs",
    "metadata",
    "playback",
    "store",
    "transport",
    "web",
  ],
  catalog: ["mediafs", "model", "store"],
  identity: ["store"],
  mediafs: [],
  metadata: ["metadata", "model", "store"],
  model: [],
  playback: ["catalog", "mediafs", "model", "store"],
  store: ["model"],
  transport: [
    "catalog",
    "identity",
    "mediafs",
    "metadata",
    "model",
    "playback",
    "store",
  ],
  web: [],
};
for (const pkg of packages) {
  const module = pkg.ImportPath.split("/")[2];
  const provider = pkg.ImportPath.endsWith("/providers");
  for (const dep of pkg.Imports || []) {
    if (dep.startsWith("reelnest/internal/")) {
      const target = dep.split("/")[2];
      if (!(provider ? ["model"] : allowed[module] || []).includes(target))
        throw new Error(`Reverse dependency: ${pkg.ImportPath} -> ${dep}`);
    }
    if (
      (dep === "database/sql" || dep.startsWith("modernc.org/sqlite")) &&
      module !== "store"
    )
      throw new Error(`SQL outside store: ${pkg.ImportPath}`);
  }
}
const appFiles = (await readdir("internal/app")).filter(
  (f) => f.endsWith(".go") && !f.endsWith("_test.go"),
);
if (appFiles.join() !== "runtime.go")
  throw new Error("App must remain assembly only");
const files = execFileSync(
  "git",
  ["ls-files", "--cached", "--others", "--exclude-standard"],
  { encoding: "utf8" },
)
  .trim()
  .split("\n");
for (const file of files) {
  if (
    !/^(internal|cmd|web\/src)\//.test(file) ||
    !/\.(go|tsx?|css|sql)$/.test(file) ||
    /_test\.go$|\.test\.tsx?$/.test(file)
  )
    continue;
  let source;
  try {
    source = await readFile(file, "utf8");
  } catch (e) {
    if (e.code === "ENOENT") continue;
    throw e;
  }
  if (
    /LegacyMetadataCredentials|ImportLegacyMetadata|optionalSecret|metadata_cache_legacy|metadata_matches_legacy|tmdbId|metadata-updated|ALTER TABLE/.test(
      source,
    )
  )
    throw new Error(`Retired compatibility implementation: ${file}`);
  if (
    /^web\/src\/[^/]+\.(tsx?|css)$/.test(file) &&
    file !== "web/src/vite-env.d.ts"
  )
    throw new Error(`Unscoped old frontend entry: ${file}`);
}
console.log(
  "Single runtime/frontend, assembly-only app, store-only SQL and module dependency directions checked.",
);
