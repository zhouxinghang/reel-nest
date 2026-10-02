import { expect, test } from "@playwright/test";
import { spawn, type ChildProcess } from "node:child_process";
import {
  copyFileSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  readdirSync,
  writeFileSync,
} from "node:fs";
import { join } from "node:path";
const origin = "http://127.0.0.1:18097";
test("正式 CLI 同步发布新 UI/API，家庭密码 _FILE、重启持久化与旧目录拒绝（仅临时目录）", async ({
  page,
}) => {
  test.setTimeout(60000);
  const root = process.env.REELNEST_TEST_ROOT!;
  const run = mkdtempSync(join(root, "production-"));
  const data = join(run, "data");
  const media = join(run, "media");
  mkdirSync(media);
  copyFileSync("tests/fixtures/playback.webm", join(media, "临时.webm"));
  const password = join(run, "password");
  writeFileSync(password, "browser-test-password\n", { mode: 0o600 });
  const env = {
    ...process.env,
    DATA_DIR: data,
    MEDIA_ROOTS: media,
    LISTEN_ADDR: "127.0.0.1:18097",
    HOUSEHOLD_USERNAME: "family",
    HOUSEHOLD_PASSWORD: "wrong-env-must-not-win",
    HOUSEHOLD_PASSWORD_FILE: password,
    COOKIE_SECURE: "false",
    TMDB_READ_ACCESS_TOKEN_FILE: join(run, "does-not-exist"),
    METATUBE_URL: "http://127.0.0.1:9/never-import",
  };
  let child: ChildProcess | undefined;
  const start = async () => {
    child = spawn(join(root, "reelnest-production"), [], {
      env,
      stdio: "inherit",
    });
    await expect
      .poll(async () => {
        if (child?.exitCode !== null) throw new Error("production exited");
        try {
          return (await fetch(`${origin}/readyz`)).status;
        } catch {
          return 0;
        }
      })
      .toBe(200);
  };
  const stop = async () => {
    if (child && child.exitCode === null)
      await new Promise<void>((done) => {
        child!.once("exit", () => done());
        child!.kill("SIGTERM");
      });
  };
  try {
    await start();
    await page.goto(origin + "/libraries");
    await page.getByLabel("家庭共享账号").fill("family");
    await page
      .getByLabel("密码", { exact: true })
      .fill("browser-test-password");
    await page.getByRole("button", { name: "进入媒体库 →" }).click();
    await expect(
      page.getByRole("heading", { name: "媒体库管理", exact: true }),
    ).toBeVisible();
    const settings = await (
      await page.request.get(origin + "/api/settings/metadata")
    ).json();
    expect(
      settings.sources.every(
        (source: { configured: boolean }) => !source.configured,
      ),
    ).toBe(true);
    const result = await page.request.post(origin + "/api/libraries", {
      data: { name: "正式入口验证", path: media, kind: "movie", sources: [] },
    });
    expect(result.status()).toBe(201);
    await stop();
    await start();
    await page.reload();
    await expect(
      page.getByRole("region", { name: "正式入口验证", exact: true }),
    ).toBeVisible();
    const response = await page.request.get(origin + "/api/directories");
    expect(response.status()).toBe(404);
    expect(await response.text()).not.toContain("<!doctype");
    const html = await (
      await page.request.get(origin + "/media/missing")
    ).text();
    expect(html).toContain("/assets/index-");
  } finally {
    await stop();
  }
  const old = join(run, "old");
  mkdirSync(old, { mode: 0o700 });
  writeFileSync(
    join(old, "reelnest.sqlite"),
    "isolated legacy/invalid database sentinel",
    { mode: 0o600 },
  );
  const failed = spawn(join(root, "reelnest-production"), [], {
    env: { ...env, DATA_DIR: old },
    stdio: ["ignore", "pipe", "pipe"],
  });
  let output = "";
  failed.stderr.on("data", (chunk) => {
    output += chunk;
  });
  const code = await new Promise<number | null>((done) =>
    failed.once("exit", done),
  );
  expect(code).not.toBe(0);
  expect(output).toContain("use a new empty DATA_DIR");
  expect(readdirSync(old)).toEqual(["reelnest.sqlite"]);
  expect(readFileSync(join(old, "reelnest.sqlite"), "utf8")).toBe(
    "isolated legacy/invalid database sentinel",
  );
});
