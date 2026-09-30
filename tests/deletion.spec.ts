import { expect, test, type Page, type BrowserContext } from "@playwright/test";
import {
  chmodSync,
  copyFileSync,
  existsSync,
  mkdirSync,
  readFileSync,
  renameSync,
  statSync,
  writeFileSync,
} from "node:fs";
import { join } from "node:path";
import { randomUUID } from "node:crypto";
import { DatabaseSync } from "node:sqlite";

import { spawn, type ChildProcess } from "node:child_process";

// Each destructive flow gets its own real production process and SQLite file.
// Never mutate the shared empty-library fixture or another suite's storage.
let application: ChildProcess;
let stopped: Promise<void>;
let dataDir: string;
test.use({ baseURL: "http://127.0.0.1:18093" });
test.beforeEach(async ({ request }) => {
  const root = process.env.REELNEST_TEST_ROOT!;
  dataDir = join(root, `deletion-data-${randomUUID()}`);
  application = spawn(join(root, "reelnest"), [], {
    env: {
      ...process.env,
      DATA_DIR: dataDir,
      MEDIA_ROOTS: join(root, "media"),
      LISTEN_ADDR: "127.0.0.1:18093",
      HOUSEHOLD_USERNAME: "family",
      HOUSEHOLD_PASSWORD: "browser-test-password",
      HOUSEHOLD_PASSWORD_FILE: "",
      COOKIE_SECURE: "false",
      TMDB_READ_ACCESS_TOKEN: "",
      TMDB_READ_ACCESS_TOKEN_FILE: "",
    },
    stdio: "inherit",
  });
  stopped = new Promise((resolve) => application.once("exit", () => resolve()));
  await expect
    .poll(async () => {
      try {
        return (await request.get("/healthz")).status();
      } catch {
        return 0;
      }
    })
    .toBe(200);
});
test.afterEach(async () => {
  if (
    !application?.pid ||
    application.exitCode !== null ||
    application.signalCode !== null
  )
    return;
  application.kill("SIGTERM");
  const force = setTimeout(() => application.kill("SIGKILL"), 10000);
  try {
    await stopped;
  } finally {
    clearTimeout(force);
  }
});

async function login(page: Page) {
  await page.goto("/login");
  await page.getByLabel("家庭共享账号", { exact: true }).fill("family");
  await page.getByLabel("密码", { exact: true }).fill("browser-test-password");
  await page.getByRole("button", { name: "进入媒体库" }).click();
  await expect(
    page.getByRole("heading", { name: "媒体库", exact: true }),
  ).toBeVisible();
}
async function awaitScan(context: BrowserContext, id: string) {
  await expect
    .poll(
      async () =>
        (
          await (await context.request.get("/api/directories")).json()
        ).directories.find((d: { id: string }) => d.id === id).scanStatus,
    )
    .toBe("completed");
}
async function addMedia(page: Page, context: BrowserContext, root: string) {
  await login(page);
  await page.goto("/directories");
  await page.getByLabel("媒体目录路径").fill(root);
  const response = page.waitForResponse(
    (r) =>
      r.url().endsWith("/api/directories") && r.request().method() === "POST",
  );
  await page.getByRole("button", { name: "添加并扫描" }).click();
  const directory = await (await response).json();
  await awaitScan(context, directory.id);
  const items = (
    await (await context.request.get("/api/library")).json()
  ).items.filter(
    (f: { directoryId: string }) => f.directoryId === directory.id,
  );
  return { directory, items };
}
function fixture() {
  const root = join(
    process.env.REELNEST_TEST_ROOT!,
    "media",
    `deletion-${randomUUID()}`,
  );
  mkdirSync(root, { recursive: true });
  const name = "仅删除这一部 中文 & 特别版.webm";
  const path = join(root, name);
  copyFileSync("tests/fixtures/playback.webm", path);
  return { root, name, path };
}

test("单文件删除：真实播放、取消/Escape、路径风险二次确认、关联文件保留", async ({
  page,
  context,
}, testInfo) => {
  const { root, name, path } = fixture();
  const subtitle = join(root, name.replace(".webm", ".srt"));
  writeFileSync(subtitle, "1\n00:00:00,000 --> 00:00:59,000\n删除测试字幕\n");
  const poster = join(root, name.replace(".webm", ".jpg"));
  writeFileSync(poster, "preserve poster");
  const other = join(root, "保留的另一个版本.webm");
  copyFileSync("tests/fixtures/playback.webm", other);
  const before = readFileSync(path);
  const { directory, items } = await addMedia(page, context, root);
  const file = items.find((f: { filename: string }) => f.filename === name);
  await page.goto("/library");
  await page.getByRole("link", { name: new RegExp(name) }).click();
  await page.getByRole("button", { name: "网页播放", exact: true }).click();
  await page.getByRole("button", { name: "开始播放", exact: true }).click();
  await expect
    .poll(() =>
      page
        .locator("video")
        .evaluate(
          (v: HTMLVideoElement) => v.getVideoPlaybackQuality().totalVideoFrames,
        ),
    )
    .toBeGreaterThan(1);
  const stream = await (
    await context.request.post(`/api/media/${file.id}/stream-link`, {
      data: {},
    })
  ).json();
  let deleteRequests = 0;
  page.on("request", (r) => {
    if (r.url().endsWith(`/api/media/${file.id}/delete`)) deleteRequests++;
  });
  const open = async () => {
    await page.getByRole("button", { name: "删除源文件", exact: true }).click();
    const dialog = page.getByRole("dialog", { name: "永久删除这一个视频？" });
    await expect(dialog.locator(".deletion-path")).toHaveText(path);
    await expect(dialog).toContainText("本系统无法恢复");
    return dialog;
  };
  let dialog = await open();
  expect(readFileSync(path)).toEqual(before);
  expect(deleteRequests).toBe(0);
  await dialog.getByRole("button", { name: "取消", exact: true }).click();
  await expect(dialog).not.toBeVisible();
  expect(readFileSync(path)).toEqual(before);
  dialog = await open();
  await page.keyboard.press("Escape");
  await expect(dialog).not.toBeVisible();
  expect(deleteRequests).toBe(0);
  expect(readFileSync(path)).toEqual(before);
  await page.setViewportSize({ width: 1440, height: 960 });
  dialog = await open();
  await page.screenshot({
    path: testInfo.outputPath("delete-confirm-desktop.png"),
  });
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(
    dialog.getByRole("button", { name: "确认永久删除" }),
  ).toBeInViewport();
  expect(await dialog.evaluate((d) => d.scrollWidth <= d.clientWidth)).toBe(
    true,
  );
  await page.screenshot({
    path: testInfo.outputPath("delete-confirm-mobile.png"),
  });
  await dialog.getByRole("button", { name: "确认永久删除" }).click();
  await expect(
    page.getByRole("region", { name: "单文件永久删除" }),
  ).toContainText("源文件已永久删除");
  expect(deleteRequests).toBe(1);
  expect(existsSync(path)).toBe(false);
  expect(readFileSync(other)).toEqual(before);
  expect(readFileSync(subtitle, "utf8")).toContain("删除测试字幕");
  expect(readFileSync(poster, "utf8")).toBe("preserve poster");
  expect(statSync(root).isDirectory()).toBe(true);
  await expect(page.locator("video")).toHaveCount(0);
  expect((await context.request.get(stream.path)).status()).toBe(410);
  await page.goto("/library");
  await expect(
    page.getByRole("link", { name: new RegExp(name) }),
  ).toContainText("不可用");
  await context.request.post(`/api/directories/${directory.id}/scan`, {
    data: {},
  });
  await awaitScan(context, directory.id);
  await page.reload();
  await expect(
    page.getByRole("link", { name: new RegExp(name) }),
  ).toContainText("不可用");
});

test("删除失败：真实权限错误及确认后目标替换不误删", async ({
  page,
  context,
}, testInfo) => {
  const { root, name, path } = fixture();
  const { items } = await addMedia(page, context, root);
  const file = items.find((f: { filename: string }) => f.filename === name);
  await page.goto(`/media/${file.id}`);
  const dialog = page.getByRole("dialog");
  await page.getByRole("button", { name: "删除源文件", exact: true }).click();
  await expect(dialog.locator(".deletion-path")).toHaveText(path);
  chmodSync(root, 0o555);
  try {
    await dialog.getByRole("button", { name: "确认永久删除" }).click();
    await expect(dialog.getByRole("alert")).toContainText("权限不足");
    expect(existsSync(path)).toBe(true);
    await expect(
      dialog.getByRole("button", { name: "确认永久删除" }),
    ).toBeDisabled();
    await page.screenshot({
      path: testInfo.outputPath("delete-permission-error.png"),
    });
  } finally {
    chmodSync(root, 0o755);
  }
  await dialog.getByRole("button", { name: "关闭", exact: true }).click();
  await page.getByRole("button", { name: "删除源文件", exact: true }).click();
  await expect(dialog.locator(".deletion-path")).toHaveText(path);
  renameSync(path, `${path}.original`);
  writeFileSync(path, "must not delete a replacement");
  await dialog.getByRole("button", { name: "确认永久删除" }).click();
  await expect(dialog.getByRole("alert")).toContainText("媒体文件已变化");
  expect(readFileSync(path, "utf8")).toBe("must not delete a replacement");
  expect(existsSync(`${path}.original`)).toBe(true);
  const detail = await (
    await context.request.get(`/api/media/${file.id}`)
  ).json();
  expect(detail.available).toBe(true);
});

test("文件已删除但真实 SQLite 写锁阻止索引更新：明确反馈并重扫修复", async ({
  page,
  context,
}, testInfo) => {
  test.setTimeout(45000);
  const { root, name, path } = fixture();
  const { directory, items } = await addMedia(page, context, root);
  const file = items.find((f: { filename: string }) => f.filename === name);
  await page.goto(`/media/${file.id}`);
  await page.getByRole("button", { name: "删除源文件", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog.locator(".deletion-path")).toHaveText(path);
  const db = new DatabaseSync(join(dataDir, "reelnest.sqlite"));
  db.exec("BEGIN IMMEDIATE");
  try {
    await dialog.getByRole("button", { name: "确认永久删除" }).click();
    await expect(
      page.getByRole("region", { name: "单文件永久删除" }).getByRole("alert"),
    ).toContainText("源文件已永久删除，但索引未更新", { timeout: 15000 });
    expect(existsSync(path)).toBe(false);
    await expect(
      page.getByRole("button", { name: "网页播放", exact: true }),
    ).toBeDisabled();
    await page.screenshot({
      path: testInfo.outputPath("delete-index-warning.png"),
    });
  } finally {
    db.exec("ROLLBACK");
    db.close();
  }
  await context.request.post(`/api/directories/${directory.id}/scan`, {
    data: {},
  });
  await awaitScan(context, directory.id);
  const detail = await (
    await context.request.get(`/api/media/${file.id}`)
  ).json();
  expect(detail.available).toBe(false);
  await page.reload();
  await expect(
    page.getByText("文件不可用：已删除或完整扫描未发现此版本", { exact: true }),
  ).toBeVisible();
});
