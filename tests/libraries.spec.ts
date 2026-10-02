import { expect, test } from "@playwright/test";
import { mkdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { randomUUID } from "node:crypto";
import { spawn, type ChildProcess } from "node:child_process";
import type { Directory, MediaFile } from "../web/src/media";

// Dedicated real application and database; never change users' media.
let application: ChildProcess;
let stopped: Promise<void>;
let dataDir: string;
test.use({ baseURL: "http://127.0.0.1:18093" });
test.beforeEach(async ({ request }) => {
  const root = process.env.REELNEST_TEST_ROOT!;
  dataDir = join(root, `libraries-data-${randomUUID()}`);
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
      METATUBE_URL: "",
      METATUBE_TOKEN: "",
      METATUBE_TOKEN_FILE: "",
      TVDB_API_KEY: "",
      TVDB_API_KEY_FILE: "",
      TVDB_PIN: "",
      TVDB_PIN_FILE: "",
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

test("具名媒体库：创建、目录分组、隔离浏览、搜索、改名、空库和窄屏", async ({
  page,
  context,
}, testInfo) => {
  await page.setViewportSize({ width: 1440, height: 1000 });
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.goto("/login");
  await page.getByLabel("家庭共享账号", { exact: true }).fill("family");
  await page.getByLabel("密码", { exact: true }).fill("browser-test-password");
  await page.getByRole("button", { name: "进入媒体库" }).click();
  await page.getByRole("link", { name: "接入第一个媒体目录" }).click();
  const root = join(
    process.env.REELNEST_TEST_ROOT!,
    "media",
    `libraries-${randomUUID()}`,
  );
  const libraries = [
    {
      name: "经典电影",
      folder: "first/movies",
      kind: "movie",
      files: ["共同片名.webm", "经典独有.webm"],
    },
    {
      name: "家庭电影",
      folder: "second/movies",
      kind: "movie",
      files: ["共同片名.webm", "家庭独有.webm"],
    },
    {
      name: "家庭电视剧",
      folder: "shows",
      kind: "series",
      files: ["星河.S01E01.webm", "星河.S01E02.webm", "无法组织.webm"],
    },
    { name: "待整理", folder: "empty", kind: "movie", files: [] },
  ];
  const created: Directory[] = [];
  for (const library of libraries) {
    const path = join(root, library.folder);
    mkdirSync(path, { recursive: true });
    for (const filename of library.files)
      writeFileSync(join(path, filename), "isolated browsing fixture");
    await page.getByLabel("媒体目录路径").fill(path);
    if (!created.length) {
      await page.getByRole("button", { name: "添加并扫描" }).click();
      expect(
        await page
          .getByLabel("媒体库名称", { exact: true })
          .evaluate((input: HTMLInputElement) => input.validity.valueMissing),
      ).toBe(true);
      expect(
        (await (await context.request.get("/api/directories")).json())
          .directories,
      ).toHaveLength(0);
    }
    await page.getByLabel("媒体库名称", { exact: true }).fill(library.name);
    await page.getByLabel("内容类型").selectOption(library.kind);
    const added = page.waitForResponse(
      (response) =>
        response.url().endsWith("/api/directories") &&
        response.request().method() === "POST",
    );
    await page.getByRole("button", { name: "添加并扫描" }).click();
    const response = await added;
    expect(response.status()).toBe(201);
    const directory = (await response.json()) as Directory;
    created.push(directory);
    const row = page.locator(".directory-row").filter({
      has: page.getByRole("heading", { name: library.name, exact: true }),
    });
    await expect(row.getByRole("status")).toHaveText("扫描完成");
    await expect(row).toContainText(path);
  }
  await page.getByRole("link", { name: "返回媒体库" }).click();
  const resources = page.getByRole("region", { name: "我的媒体" });
  await expect(resources.locator(".home-library-tile")).toHaveCount(4);
  for (const library of libraries) {
    await expect(
      resources.getByRole("link", {
        name: `浏览媒体库：${library.name}`,
        exact: true,
      }),
    ).toBeVisible();
  }
  const classicShelf = page.getByRole("region", {
    name: "最近添加 · 经典电影",
    exact: true,
  });
  await expect(classicShelf.locator(".media-card h2")).toHaveCount(2);
  await expect(classicShelf).not.toContainText("家庭独有");
  await expect(
    page
      .getByRole("region", { name: "最近添加 · 家庭电视剧", exact: true })
      .locator(".media-card h2"),
  ).toHaveCount(2);
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.screenshot({
    path: testInfo.outputPath("named-libraries-desktop.png"),
    fullPage: true,
  });
  await resources
    .getByRole("link", { name: "浏览媒体库：经典电影", exact: true })
    .click();
  await expect(page).toHaveURL(new RegExp(`directory=${created[0].id}`));
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("经典电影");
  await expect(page.locator(".media-card h2")).toHaveCount(2);
  await expect(
    page.locator(".media-card h2").filter({ hasText: "家庭独有" }),
  ).toHaveCount(0);
  await page.getByLabel("搜索文件名").fill("共同");
  await expect(page.locator(".media-card h2")).toHaveText(["共同片名.webm"]);
  await page
    .getByRole("button", { name: "添加收藏：共同片名.webm", exact: true })
    .click();
  await page.getByLabel("排序方式").selectOption("title");
  await page.getByRole("button", { name: "列表视图", exact: true }).click();
  await expect(page.locator(".home-card-grid")).toHaveClass(/home-card-list/);
  await page.reload();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("经典电影");
  await expect(page.locator(".media-card h2")).toHaveCount(2);
  await page.getByLabel("选择媒体库").selectOption(created[2].id);
  await page.getByLabel("搜索文件名").fill("星河.S01E02");
  await expect(page.locator(".media-card h2")).toHaveText(["星河"]);
  await page.locator(".media-card").click();
  await expect(
    page.getByRole("heading", { name: "星河", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("link", { name: "家庭电视剧", exact: true }),
  ).toHaveAttribute("href", `/library?directory=${created[2].id}`);
  await page.goBack();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(
    "家庭电视剧",
  );
  await page.getByLabel("选择媒体库").selectOption(created[3].id);
  await expect(
    page.getByRole("heading", { name: "这个媒体库还是空的" }),
  ).toBeVisible();
  await page.goto("/library?focus=search");
  await page.getByLabel("搜索文件名").fill("共同片名");
  await expect(page.locator(".library-group")).toHaveCount(2);
  await expect(page.locator(".media-card h2")).toHaveCount(2);
  for (const name of ["经典电影", "家庭电影"]) {
    await expect(
      page
        .getByRole("region", { name: `媒体库：${name}`, exact: true })
        .locator(".media-card h2"),
    ).toHaveText(["共同片名.webm"]);
  }
  const before = (
    await (
      await context.request.get(`/api/library?directoryId=${created[0].id}`)
    ).json()
  ).items as MediaFile[];
  await page.goto("/directories");
  const row = page
    .locator(".directory-row")
    .filter({ hasText: created[0].path });
  await row.getByRole("button", { name: "修改名称" }).click();
  await row.getByLabel("媒体库名称", { exact: true }).fill("经典与纪录片");
  await row.getByRole("button", { name: "保存名称" }).click();
  await expect(
    row.getByRole("heading", { name: "经典与纪录片", exact: true }),
  ).toBeVisible();
  const after = (
    await (
      await context.request.get(`/api/library?directoryId=${created[0].id}`)
    ).json()
  ).items;
  expect(after).toEqual(
    before.map((file) => ({ ...file, directoryName: "经典与纪录片" })),
  );
  await page.goto(`/library?type=favorites&directory=${created[0].id}`);
  await expect(page.locator(".media-card h2")).toHaveText(["共同片名.webm"]);
  for (const width of [320, 390, 768, 1440]) {
    await page.setViewportSize({ width, height: 900 });
    await page.goto("/library");
    await expect(
      page.getByRole("link", { name: "浏览媒体库：经典与纪录片", exact: true }),
    ).toBeVisible();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    if (width === 390)
      await page.screenshot({
        path: testInfo.outputPath("named-libraries-mobile.png"),
        fullPage: true,
      });
    await page.goto(`/library?directory=${created[0].id}`);
    await expect(page.locator(".media-card h2")).toHaveCount(2);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    if (width === 390)
      await page.screenshot({
        path: testInfo.outputPath("library-browse-mobile.png"),
        fullPage: true,
      });
  }
  // Equal names are display labels, never grouping keys.
  await context.request.post(`/api/directories/${created[1].id}/name`, {
    data: { name: "经典与纪录片" },
  });
  await page.goto("/library");
  await expect(
    page.getByRole("link", { name: "浏览媒体库：经典与纪录片", exact: true }),
  ).toHaveCount(2);
  await page
    .locator(`.home-library-tile[href="/library?directory=${created[1].id}"]`)
    .click();
  await expect(
    page.locator(".media-card h2").filter({ hasText: "家庭独有" }),
  ).toHaveCount(1);
  await expect(
    page.locator(".media-card h2").filter({ hasText: "经典独有" }),
  ).toHaveCount(0);
  const longName = "LongLibrary".repeat(7);
  await context.request.post(`/api/directories/${created[0].id}/name`, {
    data: { name: longName },
  });
  await page.setViewportSize({ width: 320, height: 900 });
  for (const path of [
    "/library",
    `/library?directory=${created[0].id}`,
    "/directories",
  ]) {
    await page.goto(path);
    await expect(
      page.getByText(longName, { exact: true }).first(),
    ).toBeAttached();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
  }
  await page.goto("/library?directory=missing");
  await expect(page.getByRole("alert")).toHaveText("媒体库不存在");
  await expect(page.locator(".media-card")).toHaveCount(0);
  expect(errors).toEqual([]);
});
