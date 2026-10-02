import { expect, test, type Page, type BrowserContext } from "@playwright/test";
import { copyFileSync, mkdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { randomUUID } from "node:crypto";

import { spawn, type ChildProcess } from "node:child_process";

let metadataProcess: ChildProcess;
let stopped: Promise<void>;
test.beforeEach(async ({ request }, testInfo) => {
  await request.post("http://127.0.0.1:18094/control?mode=success");
  await request.post("http://127.0.0.1:18092/control?mode=success");
  const root = process.env.REELNEST_TEST_ROOT!;
  metadataProcess = spawn(join(root, "reelnest-metadata"), [], {
    env: {
      ...process.env,
      TEST_METATUBE: testInfo.title.startsWith("MetaTube") ? "true" : "",
      TEST_TVDB: testInfo.title.startsWith("资料源") ? "true" : "",
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
async function addMedia(
  context: BrowserContext,
  name: string,
  kind = "movie",
  metadataSources = ["tmdb", "tvdb"],
) {
  const root = join(
    process.env.REELNEST_TEST_ROOT!,
    "media",
    `metadata-${randomUUID()}`,
  );
  mkdirSync(root, { recursive: true });
  copyFileSync("tests/fixtures/playback.webm", join(root, name));
  const added = await context.request.post("/api/directories", {
    data: { name: "测试媒体库", path: root, kind, metadataSources },
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

test("人物照片与本地关联作品：演员、导演、电影和电视剧导航", async ({
  page,
  context,
}, testInfo) => {
  await login(page);
  const movie = await addMedia(context, "Arrival (2016).webm");
  const episode = await addMedia(context, "Dark S01E01.webm", "series");
  await addMedia(context, "Dark S01E02.webm", "series");
  await expect
    .poll(async () => {
      const response = await context.request.get("/api/people/tmdb/100");
      return response.ok() ? (await response.json()).works.length : 0;
    })
    .toBe(2);
  await page.goto(`/media/${movie.id}`);
  const credits = page.getByRole("region", { name: "演职员", exact: true });
  await credits.scrollIntoViewIfNeeded();
  await expect
    .poll(() =>
      credits
        .getByAltText("测试主演 照片")
        .evaluate((img: HTMLImageElement) => img.naturalWidth),
    )
    .toBe(120);
  await expect
    .poll(() =>
      credits
        .getByAltText("测试导演 照片")
        .evaluate((img: HTMLImageElement) => img.naturalWidth),
    )
    .toBe(120);
  await expect(
    credits.getByRole("img", { name: "测试演员 3 暂无照片", exact: true }),
  ).toBeVisible();
  await page.evaluate(async () => {
    (document.activeElement as HTMLElement)?.blur();
    window.scrollTo({ top: 0, behavior: "instant" });
    await new Promise(requestAnimationFrame);
    await new Promise(requestAnimationFrame);
  });
  await page.screenshot({
    animations: "disabled",
    path: testInfo.outputPath("person-cards-desktop.png"),
    fullPage: true,
  });
  const actor = credits.getByRole("link", {
    name: "测试主演，查看关联作品",
    exact: true,
  });
  await actor.focus();
  await page.keyboard.press("Enter");
  await expect(page).toHaveURL(/\/people\/tmdb\/100$/);
  await expect(
    page.getByRole("heading", { name: "测试主演", exact: true }),
  ).toBeVisible();
  const works = page.getByRole("region", { name: "关联作品", exact: true });
  await expect(works.getByRole("listitem")).toHaveCount(2);
  await expect(works).toContainText("演员 · 饰 语言学家");
  await works.getByRole("button", { name: "电影", exact: true }).click();
  await expect(works.getByRole("listitem")).toHaveCount(1);
  await works.getByRole("link", { name: /降临/ }).click();
  await expect(page).toHaveURL(new RegExp(`/media/${movie.id}$`));
  await page.goBack();
  await works.getByRole("button", { name: "全部", exact: true }).click();
  await page.evaluate(async () => {
    (document.activeElement as HTMLElement)?.blur();
    window.scrollTo({ top: 0, behavior: "instant" });
    await new Promise(requestAnimationFrame);
    await new Promise(requestAnimationFrame);
  });
  await page.screenshot({
    animations: "disabled",
    path: testInfo.outputPath("person-works-desktop.png"),
    fullPage: true,
  });
  await works.getByRole("button", { name: "电视剧", exact: true }).click();
  await expect(works.getByRole("listitem")).toHaveCount(1);
  await works.getByRole("link", { name: /暗黑/ }).click();
  await expect(page).toHaveURL(/\/series\//);
  await credits
    .getByRole("link", { name: "测试导演，查看关联作品", exact: true })
    .click();
  await expect(page).toHaveURL(/\/people\/tmdb\/201$/);
  await expect(works.getByRole("listitem")).toHaveCount(2);
  await expect(works.getByRole("listitem").first()).toContainText("导演");
  await page.goto(`/media/${episode.id}`);
  await expect(credits).toContainText("整部电视剧资料，非本集专属");
  await credits
    .getByRole("link", { name: "测试主演，查看关联作品", exact: true })
    .click();
  await page.reload();
  await expect(works.getByRole("listitem")).toHaveCount(2);
  await page.setViewportSize({ width: 390, height: 844 });
  await expect
    .poll(() =>
      page.evaluate(() => document.documentElement.scrollWidth <= innerWidth),
    )
    .toBe(true);
  await page.evaluate(async () => {
    (document.activeElement as HTMLElement)?.blur();
    window.scrollTo({ top: 0, behavior: "instant" });
    await new Promise(requestAnimationFrame);
    await new Promise(requestAnimationFrame);
  });
  await page.screenshot({
    animations: "disabled",
    path: testInfo.outputPath("person-works-mobile.png"),
    fullPage: true,
  });
});

test("人物照片失败占位，关联作品与真实播放不受影响", async ({
  page,
  context,
}) => {
  await login(page);
  const movie = await addMedia(context, "Arrival (2016).webm");
  await expect
    .poll(
      async () =>
        (await (await context.request.get(`/api/media/${movie.id}`)).json())
          .match.metadata?.credits?.status,
    )
    .toBe("available");
  await context.request.post("http://127.0.0.1:18092/control?mode=photo-error");
  await page.goto(`/media/${movie.id}`);
  const credits = page.getByRole("region", { name: "演职员", exact: true });
  await credits.scrollIntoViewIfNeeded();
  await expect(
    credits.getByRole("img", { name: "测试主演 暂无照片", exact: true }),
  ).toBeVisible();
  await credits
    .getByRole("link", { name: "测试主演，查看关联作品", exact: true })
    .click();
  await expect(
    page.getByRole("heading", { name: "测试主演", exact: true }),
  ).toBeVisible();
  const works = page.getByRole("region", { name: "关联作品", exact: true });
  await works.getByRole("button", { name: "电视剧", exact: true }).click();
  await expect(works).toContainText("暂无此类型的关联作品");
  await works.getByRole("button", { name: "电影", exact: true }).click();
  await works.getByRole("link", { name: /降临/ }).click();
  await decodeVideo(page);
  await page.goto("/people/tmdb/999999");
  await expect(page.getByRole("alert")).toContainText(
    "没有此人物的有效关联作品",
  );
  await expect(
    page.getByRole("button", { name: "重新加载", exact: true }),
  ).toBeVisible();
});

test("资料源：人物照片与关联作品按来源隔离，长姓名窄屏展示", async ({
  page,
  context,
}, testInfo) => {
  await login(page);
  await addMedia(context, "Arrival (2016).webm", "movie", ["tmdb"]);
  const movie = await addMedia(context, "Arrival (2016).webm", "movie", [
    "tvdb",
  ]);
  await addMedia(context, "Dark S01E01.webm", "series", ["tvdb"]);
  await expect
    .poll(async () => {
      const response = await context.request.get("/api/people/tvdb/100");
      return response.ok() ? (await response.json()).works.length : 0;
    })
    .toBe(2);
  await page.goto(`/media/${movie.id}`);
  const credits = page.getByRole("region", { name: "演职员", exact: true });
  await credits.scrollIntoViewIfNeeded();
  await expect
    .poll(() =>
      credits
        .getByAltText("TVDB 主演 照片")
        .evaluate((img: HTMLImageElement) => img.naturalWidth),
    )
    .toBe(120);
  await expect
    .poll(() =>
      credits
        .getByAltText("TVDB 导演 照片")
        .evaluate((img: HTMLImageElement) => img.naturalWidth),
    )
    .toBe(120);
  await credits
    .getByRole("link", { name: "TVDB 主演，查看关联作品", exact: true })
    .click();
  const works = page.getByRole("region", { name: "关联作品", exact: true });
  await expect(works.getByRole("listitem")).toHaveCount(2);
  await expect(
    works.getByRole("heading", { name: "TVDB 降临", exact: true }),
  ).toBeVisible();
  await expect(
    works.getByRole("heading", { name: "降临", exact: true }),
  ).toHaveCount(0);
  await page.goto(`/media/${movie.id}`);
  await page.setViewportSize({ width: 390, height: 844 });
  await credits
    .getByRole("button", { name: "展开全部 10 位演员", exact: true })
    .click();
  await credits
    .getByRole("link", { name: /一位拥有很长很长名字的演员/ })
    .click();
  await expect(page.getByRole("heading", { level: 1 })).toContainText(
    "LongNameWithoutSpacesForMobileLayout",
  );
  await expect
    .poll(() =>
      page.evaluate(() => document.documentElement.scrollWidth <= innerWidth),
    )
    .toBe(true);
  await page.evaluate(async () => {
    (document.activeElement as HTMLElement)?.blur();
    window.scrollTo({ top: 0, behavior: "instant" });
    await new Promise(requestAnimationFrame);
    await new Promise(requestAnimationFrame);
  });
  await page.screenshot({
    animations: "disabled",
    path: testInfo.outputPath("person-long-name-mobile.png"),
    fullPage: true,
  });
});

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
  const credits = page.getByRole("region", { name: "演职员", exact: true });
  await expect(credits.getByText("测试主演", { exact: true })).toBeVisible();
  await expect(credits.getByText("饰 语言学家", { exact: true })).toBeVisible();
  await expect(
    credits.getByRole("list", { name: "导演", exact: true }),
  ).toContainText("测试导演");
  await expect(credits).not.toContainText("非导演编剧");
  await expect(
    credits
      .getByRole("list", { name: "演员", exact: true })
      .getByRole("listitem"),
  ).toHaveCount(8);
  await credits.getByRole("button", { name: "展开全部 12 位演员" }).click();
  await expect(
    credits
      .getByRole("list", { name: "演员", exact: true })
      .getByRole("listitem"),
  ).toHaveCount(12);
  await credits.getByRole("button", { name: "收起演员" }).click();
  await page.evaluate(() => window.scrollTo({ top: 0, behavior: "instant" }));
  await page.screenshot({
    path: testInfo.outputPath("credits-movie-desktop.png"),
    fullPage: true,
  });
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
  await page.getByRole("link", { name: "搜索媒体", exact: true }).click();
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
  await expect(credits).toContainText("整部电视剧资料，非本集专属");
  await expect(
    credits.getByRole("list", { name: "演员", exact: true }),
  ).toContainText("饰 角色甲 / 角色乙");
  await expect(
    credits.getByRole("list", { name: "导演", exact: true }),
  ).toContainText("测试导演");
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
  await page.goto(`/media/${episode.id}`);
  await expect(credits).toContainText("整部电视剧资料，非本集专属");
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
    await page.getByRole("button", { name: "搜索资料源", exact: true }).click();
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
  await expect(
    page.getByRole("region", { name: "演职员", exact: true }),
  ).toContainText("修正主演");
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
      page.getByRole("region", { name: "影片资料源设置与状态" }),
    ).toContainText("TMDB 服务错误");
    await page
      .getByRole("button", { name: "手动匹配影片资料", exact: true })
      .click();
    await page.getByLabel("搜索作品标题").fill("Correct");
    await page.getByRole("button", { name: "搜索资料源", exact: true }).click();
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

test("资料源：目录排序、独立配置、重载与同 ID 手动选择", async ({
  page,
  context,
}, testInfo) => {
  await login(page);
  const root = join(
    process.env.REELNEST_TEST_ROOT!,
    "media",
    `sources-${randomUUID()}`,
  );
  for (const dir of ["first", "second"]) {
    mkdirSync(join(root, dir), { recursive: true });
    copyFileSync(
      "tests/fixtures/playback.webm",
      join(root, dir, "Arrival (2016).webm"),
    );
  }
  await page.goto("/directories");
  const addForm = page.locator(".directory-form");
  await addForm
    .getByRole("button", { name: "上移 TheTVDB (TVDB)", exact: true })
    .click();
  await page.getByLabel("媒体库名称", { exact: true }).fill("first");
  await page.getByLabel("媒体目录路径").fill(join(root, "first"));
  await addForm.getByRole("button", { name: "添加并扫描" }).click();
  const firstRow = page.locator(".directory-row").filter({
    has: page.getByRole("heading", {
      name: "first",
      exact: true,
    }),
  });
  await expect(firstRow).toContainText("扫描完成");
  await expect(firstRow.locator("li").first()).toContainText("TheTVDB");
  await addForm
    .getByRole("button", { name: "上移 TheMovieDB (TMDB)", exact: true })
    .click();
  await page.getByLabel("媒体库名称", { exact: true }).fill("second");
  await page.getByLabel("媒体目录路径").fill(join(root, "second"));
  await addForm.getByRole("button", { name: "添加并扫描" }).click();
  const secondRow = page.locator(".directory-row").filter({
    has: page.getByRole("heading", {
      name: "second",
      exact: true,
    }),
  });
  await expect(secondRow).toContainText("扫描完成");
  await expect(secondRow.locator("li").first()).toContainText("TheMovieDB");
  await expect
    .poll(
      async () =>
        (await (await context.request.get("/api/metadata/status")).json())
          .running,
    )
    .toBe(false);
  const files = (await (await context.request.get("/api/library")).json())
    .items;
  const first = files.find(
    (file: { directoryPath: string }) =>
      file.directoryPath === join(root, "first"),
  );
  const second = files.find(
    (file: { directoryPath: string }) =>
      file.directoryPath === join(root, "second"),
  );
  expect(first.match.metadata.source).toBe("TVDB");
  expect(second.match.metadata.source).toBe("TMDB");
  await firstRow
    .getByRole("button", { name: "上移 TheMovieDB (TMDB)", exact: true })
    .click();
  await firstRow.getByRole("button", { name: "保存资料源" }).click();
  await expect(
    firstRow.getByRole("button", { name: "保存资料源" }),
  ).toBeDisabled();
  await expect
    .poll(
      async () =>
        (await (await context.request.get(`/api/media/${first.id}`)).json())
          .match.metadata.source,
    )
    .toBe("TMDB");
  await page.reload();
  await expect(firstRow.locator("li").first()).toContainText("TheMovieDB");
  await page.setViewportSize({ width: 390, height: 844 });
  await expect
    .poll(() =>
      page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    )
    .toBe(true);
  await page.screenshot({
    path: testInfo.outputPath("directory-sources-mobile.png"),
    fullPage: true,
  });
  await page.goto(`/media/${first.id}`);
  await page
    .getByRole("button", { name: "手动匹配影片资料", exact: true })
    .click();
  await page.getByLabel("搜索作品标题").fill("Arrival");
  await page.getByRole("button", { name: "搜索资料源", exact: true }).click();
  await expect(page.getByRole("radio")).toHaveCount(2);
  await page.getByRole("radio", { name: /TVDB #10/ }).check();
  await expect(page.getByRole("radio", { name: /TMDB #10/ })).not.toBeChecked();
  await page.getByRole("button", { name: "确认匹配", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "TVDB 降临", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("link", { name: "资料来源：TVDB ↗", exact: true }),
  ).toBeVisible();
  await expect
    .poll(() =>
      page
        .getByAltText("TVDB 降临 海报")
        .evaluate((img: HTMLImageElement) => img.naturalWidth),
    )
    .toBe(120);
  await decodeVideo(page);
});

test("资料源：TVDB 故障按优先级回退 TMDB，仍可实际播放", async ({
  page,
  context,
}) => {
  await login(page);
  await context.request.post("http://127.0.0.1:18094/control?mode=error");
  const root = join(
    process.env.REELNEST_TEST_ROOT!,
    "media",
    `fallback-${randomUUID()}`,
  );
  mkdirSync(root, { recursive: true });
  copyFileSync(
    "tests/fixtures/playback.webm",
    join(root, "Arrival (2016).webm"),
  );
  expect(
    (
      await context.request.post("/api/directories", {
        data: {
          name: "测试媒体库",
          path: root,
          kind: "movie",
          metadataSources: ["tvdb", "tmdb"],
        },
      })
    ).status(),
  ).toBe(201);
  await expect
    .poll(
      async () =>
        (await (await context.request.get("/api/library")).json()).items[0]
          ?.match.metadata?.source,
    )
    .toBe("TMDB");
  const file = (await (await context.request.get("/api/library")).json())
    .items[0];
  await page.goto(`/media/${file.id}`);
  await expect(
    page.getByRole("heading", { name: "降临", exact: true }),
  ).toBeVisible();
  await expect(page.locator(".metadata-info")).toContainText("TVDB 服务错误");
  await expect(page.locator(".metadata-info")).toContainText("已使用 TMDB");
  await decodeVideo(page);
});

test("资料源：TVDB 演员导演、窄屏长名单、空态与演职员故障仍可播放", async ({
  page,
  context,
}, testInfo) => {
  await login(page);
  const movie = await addMedia(context, "Arrival (2016).webm", "movie", [
    "tvdb",
  ]);
  await page.goto(`/media/${movie.id}`);
  const credits = page.getByRole("region", { name: "演职员", exact: true });
  await expect(credits.getByText("TVDB 主演", { exact: true })).toBeVisible();
  await expect(credits.getByText("饰 语言学家", { exact: true })).toBeVisible();
  await expect(
    credits.getByRole("list", { name: "导演", exact: true }),
  ).toContainText("TVDB 导演");
  await expect(credits).not.toContainText("非导演编剧");
  await page.setViewportSize({ width: 390, height: 844 });
  await credits.getByRole("button", { name: "展开全部 10 位演员" }).click();
  await expect(
    credits
      .getByRole("list", { name: "演员", exact: true })
      .getByRole("listitem"),
  ).toHaveCount(10);
  await expect
    .poll(() =>
      page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    )
    .toBe(true);
  await page.evaluate(() => window.scrollTo({ top: 0, behavior: "instant" }));
  await page.screenshot({
    path: testInfo.outputPath("credits-tvdb-mobile.png"),
    fullPage: true,
  });
  const episode = await addMedia(context, "Dark S01E01.webm", "series", [
    "tvdb",
  ]);
  await page.goto(`/series/${episode.episode.seriesId}`);
  await expect(credits.getByText("TVDB 主演", { exact: true })).toBeVisible();
  await expect(credits).toContainText("整部电视剧资料，非本集专属");
  await expect
    .poll(
      async () =>
        (await (await context.request.get("/api/metadata/status")).json())
          .running,
    )
    .toBe(false);
  const selectionURL = `/api/matching/series/${episode.episode.seriesId}/selection`;
  await context.request.post(
    "http://127.0.0.1:18094/control?mode=credits-empty",
  );
  expect(
    (
      await context.request.post(selectionURL, {
        data: { kind: "series", source: "tvdb", sourceId: 10 },
      })
    ).status(),
  ).toBe(200);
  await page.reload();
  await expect(credits).toContainText("暂无导演信息");
  await expect(credits).toContainText("暂无演员信息");
  await context.request.post(
    "http://127.0.0.1:18094/control?mode=credits-error",
  );
  expect(
    (
      await context.request.post(selectionURL, {
        data: { kind: "series", source: "tvdb", sourceId: 10 },
      })
    ).status(),
  ).toBe(200);
  await page.goto(`/media/${episode.id}`);
  await expect(credits).toContainText("演职员信息暂时无法获取");
  await expect(page.locator(".metadata-info")).toContainText(
    "TVDB 中文隔离测试资料",
  );
  await expect(credits).not.toContainText("暂无演员信息");
  await decodeVideo(page);
});

for (const source of ["tmdb", "tvdb"] as const) {
  test(`资料源多类型图片：${source} 海报、横版封面和详情背景`, async ({
    page,
    context,
  }, testInfo) => {
    await login(page);
    const file = await addMedia(context, "Arrival (2016).webm", "movie", [
      source,
    ]);
    await expect
      .poll(
        async () =>
          (await (await context.request.get(`/api/media/${file.id}`)).json())
            .match.metadata?.backdropUrl,
      )
      .toBeTruthy();
    const media = await (
      await context.request.get(`/api/media/${file.id}`)
    ).json();
    const metadata = media.match.metadata;
    await context.request.post(`/api/media/${file.id}/progress`, {
      data: { position: 12, duration: 60, completed: false },
    });
    await page.goto("/");
    const cover = page.locator(".continue-art img");
    await expect(cover).toHaveAttribute("src", metadata.thumbUrl);
    await expect
      .poll(() => cover.evaluate((img: HTMLImageElement) => img.naturalWidth))
      .toBe(640);
    await expect(page.locator(".home-media-card img").first()).toHaveAttribute(
      "src",
      metadata.posterUrl,
    );
    await page.goto(`/media/${file.id}`);
    await expect(page.locator(".detail-poster img")).toHaveAttribute(
      "src",
      metadata.posterUrl,
    );
    const backdrop = page.locator(".detail-backdrop img");
    await expect(backdrop).toHaveAttribute("src", metadata.backdropUrl);
    await expect
      .poll(() =>
        backdrop.evaluate((img: HTMLImageElement) => img.naturalWidth),
      )
      .toBe(640);
    await page.screenshot({
      path: testInfo.outputPath(`artwork-${source}-desktop.png`),
    });
    await page.setViewportSize({ width: 390, height: 844 });
    await page.evaluate(async () => {
      window.scrollTo({ top: 0, behavior: "instant" });
      await new Promise(requestAnimationFrame);
      await new Promise(requestAnimationFrame);
    });
    await expect(page.locator(".app-header")).toHaveCount(1);
    await expect(
      page.getByRole("heading", { name: metadata.title, exact: true }),
    ).toBeVisible();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    await page.screenshot({
      path: testInfo.outputPath(`artwork-${source}-mobile.png`),
    });
    // A broken backdrop falls back to the independently cached cover, never to
    // a stretched portrait. All broken landscapes restore the plain canvas.
    await page.route(`**${metadata.backdropUrl}`, (route) =>
      route.fulfill({ status: 404 }),
    );
    await page.reload();
    await expect(page.locator(".detail-backdrop img")).toHaveAttribute(
      "src",
      metadata.thumbUrl,
    );
    await page.route(`**${metadata.thumbUrl}`, (route) =>
      route.fulfill({ status: 404 }),
    );
    await page.reload();
    await expect(page.locator(".detail-backdrop")).toHaveCount(0);
    await expect(page.locator(".detail-poster img")).toBeVisible();
    await page.goto("/");
    await expect(page.locator(".continue-art img")).toHaveAttribute(
      "src",
      metadata.posterUrl,
    );
    await expect(page.locator(".continue-art img")).toHaveCSS(
      "object-fit",
      "contain",
    );
    // Restore real HTTP and verify series and organized episodes inherit the
    // title's distinct image roles rather than manufacturing episode stills.
    await page.unrouteAll();
    const episode = await addMedia(context, "Dark S01E01.webm", "series", [
      source,
    ]);
    await expect
      .poll(
        async () =>
          (await (await context.request.get(`/api/media/${episode.id}`)).json())
            .match.metadata?.backdropUrl,
      )
      .toBeTruthy();
    const detail = await (
      await context.request.get(`/api/media/${episode.id}`)
    ).json();
    for (const path of [
      `/series/${detail.episode.seriesId}`,
      `/media/${episode.id}`,
    ]) {
      await page.goto(path);
      await expect(page.locator(".detail-backdrop img")).toHaveAttribute(
        "src",
        detail.match.metadata.backdropUrl,
      );
      await expect(page.locator(".detail-poster img")).toHaveAttribute(
        "src",
        detail.match.metadata.posterUrl,
      );
    }
  });
}

test("多类型图片缺失仍可浏览播放且不把海报拉伸成背景", async ({
  page,
  context,
  request,
}) => {
  await request.post("http://127.0.0.1:18092/control?mode=artwork-empty");
  await login(page);
  const file = await addMedia(context, "Arrival (2016).webm");
  await expect
    .poll(
      async () =>
        (await (await context.request.get(`/api/media/${file.id}`)).json())
          .match.metadata?.artwork?.status,
    )
    .toBe("available");
  await page.goto(`/media/${file.id}`);
  await expect(page.locator(".detail-poster img")).toBeVisible();
  await expect(page.locator(".detail-backdrop")).toHaveCount(0);
  await decodeVideo(page);
});

test("MetaTube 目录启用、编号匹配、图片、人物及手动选择", async ({
  page,
  context,
}, testInfo) => {
  await login(page);
  const file = await addMedia(context, "ABC-123 (2024).webm", "movie", [
    "metatube",
  ]);
  await expect
    .poll(
      async () =>
        (await (await context.request.get("/api/metadata/status")).json())
          .running,
    )
    .toBe(false);
  await page.goto("/directories");
  // The integration is opt-in: new-directory defaults still only select TMDB/TVDB.
  const orders = page.getByRole("group", { name: "影片资料源优先级" });
  await expect(
    orders.first().getByRole("checkbox", { name: /MetaTube/ }),
  ).not.toBeChecked();
  await expect(
    orders.last().getByRole("checkbox", { name: /MetaTube/ }),
  ).toBeChecked();
  await expect(
    page.getByRole("link", { name: "影片资料源配置", exact: true }),
  ).toBeVisible();
  await orders
    .last()
    .getByRole("checkbox", { name: /TheMovieDB/ })
    .check();
  await page.getByRole("button", { name: "保存资料源", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "保存资料源", exact: true }),
  ).toBeDisabled();
  await page.setViewportSize({ width: 390, height: 844 });
  await expect
    .poll(() =>
      page.evaluate(() => document.documentElement.scrollWidth <= innerWidth),
    )
    .toBe(true);
  await page.evaluate(async () => {
    (document.activeElement as HTMLElement)?.blur();
    window.scrollTo({ top: 0, behavior: "instant" });
    await new Promise(requestAnimationFrame);
    await new Promise(requestAnimationFrame);
  });
  await page.screenshot({
    path: testInfo.outputPath("metatube-directory-mobile.png"),
    fullPage: true,
  });
  await page.goto(`/media/${file.id}`);
  await expect(
    page.getByRole("heading", { name: "MetaTube 示例影片", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("MetaTube 隔离接口测试简介", { exact: true }),
  ).toBeVisible();
  await expect
    .poll(() =>
      page
        .getByAltText("MetaTube 示例影片 海报")
        .evaluate((img: HTMLImageElement) => img.naturalWidth),
    )
    .toBe(120);
  await expect
    .poll(() =>
      page
        .locator(".detail-backdrop img")
        .evaluate((img: HTMLImageElement) => img.naturalWidth),
    )
    .toBeGreaterThan(0);
  const credits = page.getByRole("region", { name: "演职员", exact: true });
  await credits
    .getByRole("link", { name: "MetaTube 演员，查看关联作品", exact: true })
    .click();
  await expect(page).toHaveURL(/\/people\/metatube\/local-/);
  await expect(
    page.getByRole("heading", { name: "MetaTube 演员", exact: true }),
  ).toBeVisible();
  await page.goto(`/media/${file.id}`);
  await page
    .getByRole("button", { name: "手动匹配影片资料", exact: true })
    .click();
  await page.getByLabel("搜索作品标题", { exact: true }).fill("选择");
  await page.getByRole("button", { name: "搜索资料源", exact: true }).click();
  await page.getByRole("radio", { name: /ABC-123.*MetaTube \/ Other/ }).check();
  await page.getByRole("button", { name: "确认匹配", exact: true }).click();
  await expect(
    page.getByRole("link", { name: /资料来源：MetaTube \/ other/i }),
  ).toBeVisible();
  await expect(page.getByText(/手动匹配 · 缓存有效至/)).toBeVisible();
  const response = await context.request.get(`/api/media/${file.id}`);
  expect(JSON.stringify(await response.json())).not.toContain(
    "isolated-browser-metatube-token",
  );
});

test("页面配置影片资料源：即时生效、凭据隐藏、冲突恢复与窄屏", async ({
  page,
  context,
}, testInfo) => {
  await login(page);
  await page.goto("/directories");
  await page.getByRole("link", { name: "影片资料源配置", exact: true }).click();
  await expect(page).toHaveURL(/\/settings\/metadata$/);
  const tmdb = page.getByRole("form", { name: "TheMovieDB 配置", exact: true });
  const tvdb = page.getByRole("form", { name: "TheTVDB 配置", exact: true });
  const metatube = page.getByRole("form", {
    name: "MetaTube 配置",
    exact: true,
  });
  await expect(
    tmdb.getByLabel("TMDB API Read Access Token", { exact: true }),
  ).toHaveValue("");
  await expect(tmdb).toContainText("凭据已保存，不回显");
  await tmdb
    .getByLabel("清除已保存的 TMDB API Read Access Token", { exact: true })
    .check();
  await tmdb.getByRole("button", { name: "保存配置", exact: true }).click();
  await expect(tmdb).toContainText("尚未配置服务");
  await tmdb
    .getByLabel("TMDB API Read Access Token", { exact: true })
    .fill("isolated-browser-token");
  await tmdb.getByRole("button", { name: "保存配置", exact: true }).click();
  await expect(tmdb).toContainText("凭据已保存，不回显");
  await tvdb
    .getByLabel("TVDB API Key", { exact: true })
    .fill("isolated-browser-tvdb-key");
  await tvdb
    .getByLabel("TVDB PIN", { exact: true })
    .fill("isolated-browser-pin");
  await tvdb.getByRole("button", { name: "保存配置", exact: true }).click();
  await expect(tvdb).toContainText("PIN 已保存，不回显");
  await metatube
    .getByLabel("MetaTube 服务地址", { exact: true })
    .fill("http://127.0.0.1:18095");
  await metatube
    .getByLabel("MetaTube Token", { exact: true })
    .fill("isolated-browser-metatube-token");
  await metatube.getByRole("button", { name: "保存配置", exact: true }).click();
  await expect(metatube).toContainText("凭据已保存，不回显");
  await page.reload();
  for (const label of [
    "TMDB API Read Access Token",
    "TVDB API Key",
    "TVDB PIN",
    "MetaTube Token",
  ]) {
    await expect(page.getByLabel(label, { exact: true })).toHaveValue("");
    await expect(page.getByLabel(label, { exact: true })).toHaveAttribute(
      "type",
      "password",
    );
  }
  await expect(
    metatube.getByLabel("MetaTube 服务地址", { exact: true }),
  ).toHaveValue("http://127.0.0.1:18095");
  const settings = await (
    await context.request.get("/api/settings/metadata")
  ).json();
  for (const secret of [
    "isolated-browser-token",
    "isolated-browser-tvdb-key",
    "isolated-browser-pin",
    "isolated-browser-metatube-token",
  ]) {
    expect(JSON.stringify(settings)).not.toContain(secret);
    expect(await page.content()).not.toContain(secret);
  }
  // Editing an endpoint must not forward the saved Token to a different service.
  await metatube
    .getByLabel("MetaTube 服务地址", { exact: true })
    .fill("http://127.0.0.1:18095/other");
  await metatube.getByRole("button", { name: "保存配置", exact: true }).click();
  await expect(metatube.getByRole("alert")).toContainText(
    "重新填写或明确清除 Token",
  );
  await page
    .getByRole("button", { name: "重新加载配置", exact: false })
    .click();
  await expect(
    metatube.getByLabel("MetaTube 服务地址", { exact: true }),
  ).toHaveValue("http://127.0.0.1:18095");

  // A failed save preserves the user's input and does not pretend to succeed.
  await page.route("**/api/settings/metadata/tvdb", (route) =>
    route.fulfill({
      status: 503,
      contentType: "application/json",
      body: JSON.stringify({ error: "配置保存暂不可用" }),
    }),
  );
  await tvdb.getByLabel("TVDB PIN", { exact: true }).fill("retry-pin");
  await tvdb.getByRole("button", { name: "保存配置", exact: true }).click();
  await expect(tvdb.getByRole("alert")).toHaveText("配置保存暂不可用");
  await expect(tvdb.getByLabel("TVDB PIN", { exact: true })).toHaveValue(
    "retry-pin",
  );
  await page.unroute("**/api/settings/metadata/tvdb");
  await tvdb.getByLabel("清除已保存的 TVDB PIN", { exact: true }).check();
  await tvdb.getByRole("button", { name: "保存配置", exact: true }).click();
  await expect(tvdb).toContainText("未保存 PIN");

  const other = await context.newPage();
  try {
    await other.goto("/settings/metadata");
    const otherTMDB = other.getByRole("form", {
      name: "TheMovieDB 配置",
      exact: true,
    });
    await expect(otherTMDB).toContainText("凭据已保存，不回显");
    await tmdb
      .getByLabel("清除已保存的 TMDB API Read Access Token", { exact: true })
      .check();
    await tmdb.getByRole("button", { name: "保存配置", exact: true }).click();
    await expect(tmdb).toContainText("尚未配置服务");
    await otherTMDB
      .getByLabel("TMDB API Read Access Token", { exact: true })
      .fill("isolated-browser-token");
    await otherTMDB
      .getByRole("button", { name: "保存配置", exact: true })
      .click();
    await expect(otherTMDB.getByRole("alert")).toContainText(
      "配置已被其他页面修改",
    );
    await other
      .getByRole("button", { name: "重新加载配置", exact: false })
      .click();
    await expect(otherTMDB).toContainText("尚未配置服务");
    await otherTMDB
      .getByLabel("TMDB API Read Access Token", { exact: true })
      .fill("isolated-browser-token");
    await otherTMDB
      .getByRole("button", { name: "保存配置", exact: true })
      .click();
    await expect(otherTMDB).toContainText("凭据已保存，不回显");
  } finally {
    await other.close();
  }
  await page.reload();
  await expect(tmdb).toContainText("凭据已保存，不回显");
  await page.bringToFront();
  for (const width of [1440, 390, 320]) {
    await page.setViewportSize({ width, height: 1000 });
    await page.evaluate(async () => {
      (document.activeElement as HTMLElement)?.blur();
      window.scrollTo(0, 0);
      await new Promise((resolve) =>
        requestAnimationFrame(() => requestAnimationFrame(resolve)),
      );
    });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    if (width !== 320)
      await page.screenshot({
        path: testInfo.outputPath(`metadata-settings-${width}.png`),
        fullPage: true,
      });
  }
  // Real upstream fixtures now work with settings saved from the page, no restart.
  const file = await addMedia(context, "ABC-123.webm", "movie", ["metatube"]);
  await expect
    .poll(
      async () =>
        (await (await context.request.get(`/api/media/${file.id}`)).json())
          .match.metadata?.source,
    )
    .toBe("MetaTube");
  const movie = await addMedia(context, "Arrival (2016).webm", "movie", [
    "tvdb",
  ]);
  await expect
    .poll(
      async () =>
        (await (await context.request.get(`/api/media/${movie.id}`)).json())
          .match.metadata?.source,
    )
    .toBe("TVDB");
});
