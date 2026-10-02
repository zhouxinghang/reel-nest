import { readFile, readdir } from "node:fs/promises";
import { basename } from "node:path";

const inventory = JSON.parse(
  await readFile("docs/architecture/regression-inventory.json", "utf8"),
);
const expected = new Map();
for (const entry of inventory.entries) {
  const key = `${entry.file}:${entry.test}`;
  if (expected.has(key)) throw new Error(`Duplicate inventory: ${key}`);
  if (
    entry.state !== "present" ||
    !entry.requirements.length ||
    entry.requirements.some((r) => !/^R(0[1-9]|1[0-2])$/.test(r)) ||
    !entry.notes ||
    ![
      "rewrite-contract",
      "retain-harness",
      "split-preserve",
      "remove-legacy-only",
    ].includes(entry.action)
  )
    throw new Error(`Incomplete review record: ${key}`);
  expected.set(key, entry);
}
const actual = new Set();
for (const directory of ["internal/app", "cmd/reelnest", "tests"]) {
  for (const name of await readdir(directory)) {
    if (!/(_test\.go|\.spec\.ts)$/.test(basename(name))) continue;
    const file = `${directory}/${name}`;
    const source = await readFile(file, "utf8");
    const pattern = name.endsWith(".go")
      ? /^func (Test\w+)\(/gm
      : /\btest\(\s*["`]([^"`]+)["`]/g;
    for (const match of source.matchAll(pattern))
      actual.add(`${file}:${match[1]}`);
  }
}
for (const key of actual)
  if (!expected.has(key)) throw new Error(`Unclassified legacy test: ${key}`);
for (const key of expected.keys())
  if (!actual.has(key))
    throw new Error(
      `Baseline regression removed without migration review: ${key}`,
    );
console.log(`${actual.size} baseline test declarations preserved and traced.`);
