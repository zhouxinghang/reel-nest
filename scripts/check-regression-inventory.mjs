import { readFile, readdir } from "node:fs/promises";
import { join } from "node:path";
const inventory = JSON.parse(
  await readFile("docs/architecture/regression-inventory.json", "utf8"),
);
async function walk(dir) {
  const files = [];
  for (const entry of await readdir(dir, { withFileTypes: true })) {
    const file = join(dir, entry.name);
    if (entry.isDirectory()) files.push(...(await walk(file)));
    else if (/(_test\.go|\.(spec|test)\.tsx?)$/.test(file)) files.push(file);
  }
  return files;
}
const declarations = new Map();
for (const file of (
  await Promise.all(["internal", "cmd", "tests", "web/src"].map(walk))
).flat()) {
  const source = await readFile(file, "utf8");
  const pattern = file.endsWith(".go")
    ? /^func (Test\w+)\(/gm
    : /\b(?:test|it)\(\s*["`]([^"`]+)["`]/g;
  declarations.set(
    file,
    new Set([...source.matchAll(pattern)].map((m) => m[1])),
  );
}
const keys = new Set();
let retired = 0;
if (inventory.entries.length !== 97)
  throw new Error("Baseline must retain all 97 review records");
for (const e of inventory.entries) {
  const key = `${e.file}:${e.test}`;
  if (
    keys.has(key) ||
    !e.requirements.length ||
    e.requirements.some((r) => !/^R(0[1-9]|1[0-2])$/.test(r)) ||
    !e.notes ||
    !e.resolution
  )
    throw new Error(`Incomplete review: ${key}`);
  keys.add(key);
  if (declarations.get(e.file)?.has(e.test))
    throw new Error(`Old runtime test was retained: ${key}`);
  if (
    e.state === "retired" &&
    e.action === "remove-legacy-only" &&
    e.evidence.length === 0
  ) {
    retired++;
    continue;
  }
  if (e.state !== "migrated" || !e.evidence?.length)
    throw new Error(`Missing replacement: ${key}`);
  for (const proof of e.evidence) {
    const tests = declarations.get(proof.file);
    if (!tests || (proof.suite ? !tests.size : !tests.has(proof.test)))
      throw new Error(
        `Replacement is not executable: ${key} -> ${JSON.stringify(proof)}`,
      );
  }
}
if (retired !== 4)
  throw new Error("Only the four pure legacy migrations may be retired");
console.log(
  `97 baseline records reviewed: ${97 - retired} migrated with executable evidence, ${retired} pure legacy migrations retired.`,
);
