import { expect, test, type Page, type BrowserContext } from "@playwright/test";
import { copyFileSync, mkdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { randomUUID } from "node:crypto";

import { spawn, type ChildProcess } from "node:child_process";

let metadataProcess: ChildProcess;
let stopped: Promise<void>;
test.beforeEach(async ({ request }) => {
  await request.post("http://127.0.0.1:18092/control?mode=success");
  const root = process.env.REELNEST_TEST_ROOT!;
  metadataProcess = spawn(join(root, "reelnest-metadata"), [], {
    env: {
      ...process.env,
      DATA_DIR: join(root, `metadata-data-${randomUUID()}`),
      MEDIA_ROOTS: join(root, "media"),
    },
    stdio: "inherit",
  });
  stopped = new Promise((resolve) =>
    metadataProcess.once("exit", () => resolve()),
  );
});
test.afterEach(async () => {
  metadataProcess?.kill("SIGTERM");
  if (stopped) await stopped;
});

test.use({ baseURL: "http://127.0.0.1:18091" });
async function login(page: Page) {
  await expect
    .poll(async () => {
      try {
        return (await page.request.get("/healthz")).status();
      } catch {
        return 0;
      }
    })
    .toBe(200);
  await page.goto("/login");
  await page.getByLabel("家庭共享账号", { exact: true }).fill("family");
  await page.getByLabel("密码", { exact: true }).fill("browser-test-password");
  await page.getByRole("button", { name: "进入媒体库" }).click();
  await expect(
    page.getByRole("heading", { name: "媒体库", exact: true }),
  ).toBeVisible();
}
async function addMedia(context: BrowserContext, name: string, kind = "movie") {
  const root = join(
    process.env.REELNEST_TEST_ROOT!,
    "media",
    `metadata-${randomUUID()}`,
  );
  mkdirSync(root, { recursive: true });
  copyFileSync("tests/fixtures/playback.webm", join(root, name));
  const added = await context.request.post("/api/directories", {
    data: { path: root, kind },
  });
  expect(added.status()).toBe(201);
  const directory = await added.json();
  await expect
    .poll(
      async () =>
        (
          await (await context.request.get("/api/directories")).json()
        ).directories.find((d: { id: string }) => d.id === directory.id)
          .scanStatus,
    )
    .toBe("completed");
  const items = (await (await context.request.get("/api/library")).json())
    .items;
  return items.find(
    (f: { directoryId: string }) => f.directoryId === directory.id,
  );
}
async function decodeVideo(page: Page) {
  await page.getByRole("button", { name: "网页播放", exact: true }).click();
  await expect
    .poll(() =>
      page.locator("video").evaluate((v: HTMLVideoElement) => v.duration),
    )
    .toBe(60);
  await expect(
    page.getByRole("button", { name: "暂停", exact: true }),
  ).toBeVisible();
  await expect
    .poll(() =>
      page
        .locator("video")
        .evaluate(
          (v: HTMLVideoElement) => v.getVideoPlaybackQuality().totalVideoFrames,
        ),
    )
    .toBeGreaterThan(2);
  await expect
    .poll(() =>
      page.locator("video").evaluate((v: HTMLVideoElement) => v.currentTime),
    )
    .toBeGreaterThan(0.3);
  await page.getByRole("button", { name: "暂停", exact: true }).click();
}

test("TMDB 成功匹配的海报墙、中文详情、电视剧与真实播放", async ({
  page,
  context,
}, testInfo) => {
  await login(page);
  const file = await addMedia(context, "Arrival (2016).webm");
  await page.goto(`/media/${file.id}`);
  await expect(
    page.getByRole("heading", { name: "降临", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("中文优先的隔离测试简介", { exact: false }),
  ).toBeVisible();
  await expect
    .poll(() =>
      page
        .getByAltText("降临 海报")
        .evaluate((img: HTMLImageElement) => img.naturalWidth),
    )
    .toBe(120);
  await decodeVideo(page);
  await page.goto("/library");
  await expect(
    page.locator(`a[href="/media/${file.id}"]`).filter({
      has: page.getByRole("heading", { name: "降临", exact: true }),
    }),
  ).toBeVisible();
  await page.getByLabel("搜索文件名").fill("降临");
  await expect(
    page.getByRole("heading", { name: "降临", exact: true }),
  ).toBeVisible();
  await page.screenshot({
    path: testInfo.outputPath("metadata-library.png"),
    fullPage: true,
  });
  const episode = await addMedia(context, "Dark S01E01.webm", "series");
  await page.goto(`/series/${episode.episode.seriesId}`);
  await expect(
    page.getByRole("heading", { name: "暗黑", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("link", { name: "第 1 季", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("link", { name: /Dark S01E01.webm/ }),
  ).toBeVisible();
  await page.setViewportSize({ width: 390, height: 844 });
  await expect
    .poll(() =>
      page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    )
    .toBe(true);
  await page.screenshot({
    path: testInfo.outputPath("metadata-series-mobile.png"),
    fullPage: true,
  });
});

test("手动匹配搜索、取消、无结果、确认、海报墙与电视剧更新", async ({
  page,
  context,
}, testInfo) => {
  await login(page);
  const file = await addMedia(context, "Arrival (2016).webm");
  await page.goto(`/media/${file.id}`);
  await expect(
    page.getByRole("heading", { name: "降临", exact: true }),
  ).toBeVisible();
  await expect
    .poll(
      async () =>
        (await (await context.request.get("/api/metadata/status")).json())
          .running,
    )
    .toBe(false);
  const original = readFileSync(file.path);
  await context.request.post(`/api/media/${file.id}/progress`, {
    data: { position: 18, duration: 60, completed: false },
  });
  const open = async () =>
    page.getByRole("button", { name: "手动匹配影片资料", exact: true }).click();
  const search = async (query: string) => {
    await page.getByLabel("搜索作品标题").fill(query);
    await page.getByRole("button", { name: "搜索 TMDB", exact: true }).click();
  };
  await open();
  await search("Correct");
  await page.getByRole("radio", { name: /正确作品/ }).check();
  await page.getByRole("button", { name: "取消", exact: true }).click();
  await expect(page.getByRole("dialog")).not.toBeVisible();
  expect(
    (await (await context.request.get(`/api/media/${file.id}`)).json()).match
      .metadata.tmdbId,
  ).toBe(10);
  await open();
  await search("Nothing");
  await expect(page.getByText("没有找到候选", { exact: false })).toBeVisible();
  await expect(
    page.getByRole("button", { name: "确认匹配", exact: true }),
  ).toBeDisabled();
  await search("Correct");
  await page.getByRole("radio", { name: /正确作品/ }).check();
  await page.setViewportSize({ width: 390, height: 844 });
  await expect
    .poll(() =>
      page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    )
    .toBe(true);
  await page.screenshot({
    path: testInfo.outputPath("manual-matching-mobile.png"),
    fullPage: true,
  });
  await page.getByRole("button", { name: "确认匹配", exact: true }).click();
  await expect(page.getByRole("dialog")).not.toBeVisible();
  await expect(
    page.getByRole("heading", { name: "正确作品", exact: true }),
  ).toBeVisible();
  const changed = await (
    await context.request.get(`/api/media/${file.id}`)
  ).json();
  expect(changed.id).toBe(file.id);
  expect(changed.progress.position).toBe(18);
  expect(changed.match.selection).toBe("manual");
  expect(readFileSync(file.path).equals(original)).toBe(true);
  await page.goto("/library");
  await expect(
    page.locator(`a[href="/media/${file.id}"]`).filter({
      has: page.getByRole("heading", { name: "正确作品", exact: true }),
    }),
  ).toBeVisible();
  const episode = await addMedia(context, "Dark S01E01.webm", "series");
  await page.goto(`/series/${episode.episode.seriesId}`);
  await expect(
    page.getByRole("heading", { name: "暗黑", exact: true }),
  ).toBeVisible();
  await expect
    .poll(
      async () =>
        (await (await context.request.get("/api/metadata/status")).json())
          .running,
    )
    .toBe(false);
  await open();
  await search("Correct");
  await expect(
    page.getByRole("radio", { name: /正确作品/ }),
  ).toHaveAccessibleName(/电视剧/);
  await page.getByRole("radio", { name: /正确作品/ }).check();
  await page.getByRole("button", { name: "确认匹配", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "正确作品", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("link", { name: /Dark S01E01.webm/ }),
  ).toBeVisible();
});

test("TMDB 服务失败仍能扫描、浏览并真实解码本地视频", async ({
  page,
  context,
}) => {
  await login(page);
  await expect
    .poll(
      async () =>
        (await (await context.request.get("/api/metadata/status")).json())
          .running,
    )
    .toBe(false);
  await context.request.post("http://127.0.0.1:18092/control?mode=error");
  try {
    const file = await addMedia(context, "Failure (2020).webm");
    await page.goto(`/media/${file.id}`);
    await expect(
      page.getByRole("heading", { name: "Failure (2020).webm", exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("region", { name: "TMDB 设置与状态" }),
    ).toContainText("TMDB 服务错误");
    await page
      .getByRole("button", { name: "手动匹配影片资料", exact: true })
      .click();
    await page.getByLabel("搜索作品标题").fill("Correct");
    await page.getByRole("button", { name: "搜索 TMDB", exact: true }).click();
    await expect(page.getByRole("dialog").getByRole("alert")).toContainText(
      "原有资料关联未改变",
    );
    await page.getByRole("button", { name: "取消", exact: true }).click();
    await decodeVideo(page);
    expect(
      (await (await context.request.get(`/api/media/${file.id}`)).json())
        .matched,
    ).toBe(false);
  } finally {
    await context.request.post("http://127.0.0.1:18092/control?mode=success");
  }
});
