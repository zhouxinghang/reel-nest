import { readFile, writeFile } from "node:fs/promises";
import openapiTS, { astToString } from "openapi-typescript";
import prettier from "prettier";

const schema = new URL("../api/openapi.yaml", import.meta.url);
const target = new URL("../web/src/generated/api.d.ts", import.meta.url);
const generated = await prettier.format(
  "/** Generated from api/openapi.yaml. Do not edit. */\n" +
    astToString(await openapiTS(schema)),
  { parser: "typescript" },
);
if (process.argv.includes("--check")) {
  if ((await readFile(target, "utf8")) !== generated) {
    throw new Error("Contract types drifted; run npm run contracts:generate");
  }
  console.log("Contract types are reproducible and current.");
} else {
  await writeFile(target, generated);
}
