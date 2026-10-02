import { expect, test, type Page } from "@playwright/test";
import { spawn, type ChildProcess } from "node:child_process";
import { copyFileSync, mkdirSync, mkdtempSync } from "node:fs";
import { join } from "node:path";
import type { components } from "../../web/src/generated/api";
type Schema = components["schemas"];
const origin = "http://127.0.0.1:18096";
let child: ChildProcess;
let media: string;
test.beforeEach(async ({ page }) => {
  const root = process.env.REELNEST_TEST_ROOT!;
  const run = mkdtempSync(join(root, "providers-ui-"));
  media = join(run, "media");
  mkdirSync(join(media, "movies"), { recursive: true });
  mkdirSync(join(media, "tv", "Dark"), { recursive: true });
  copyFileSync(
    "tests/fixtures/playback.webm",
    join(media, "movies", "Arrival (2016).webm"),
  );
  copyFileSync(
    "tests/fixtures/playback.webm",
    join(media, "tv", "Dark", "01.webm"),
  );
  for (const port of [18092, 18094])
    await page.request.post(`http://127.0.0.1:${port}/control?mode=success`);
  child = spawn(join(root, "reelnest-runtime"), [], {
    env: { ...process.env, DATA_DIR: join(run, "data"), MEDIA_ROOTS: media },
    stdio: "inherit",
  });
  await expect
    .poll(async () => {
      if (child.exitCode !== null) throw new Error("runtime exited");
      try {
        return (await fetch(origin + "/readyz")).status;
      } catch {
        return 0;
      }
    })
    .toBe(200);
  expect(
    (
      await page.request.post(origin + "/api/login", {
        data: { username: "family", password: "browser-test-password" },
      })
    ).status(),
  ).toBe(200);
  for (const [source, credential] of [
    ["tmdb", "isolated-browser-token"],
    ["tvdb", "isolated-browser-tvdb-key"],
  ])
    expect(
      (
        await page.request.patch(`${origin}/api/settings/metadata/${source}`, {
          data: { revision: 1, credential },
        })
      ).status(),
    ).toBe(200);
});
test.afterEach(async () => {
  if (child && child.exitCode === null)
    await new Promise<void>((done) => {
      child.once("exit", () => done());
      child.kill("SIGTERM");
    });
});
async function add(page: Page, kind: "movie" | "series", sources: string[]) {
  const result = await page.request.post(origin + "/api/libraries", {
    data: {
      name: kind,
      path: join(media, kind === "movie" ? "movies" : "tv"),
      kind,
      sources,
    },
  });
  expect(result.status()).toBe(201);
  const library: Schema["Library"] = await result.json();
  await expect
    .poll(
      async () =>
        (
          await (
            await page.request.get(
              `${origin}/api/scans/${library.latestScanId}`,
            )
          ).json()
        ).state,
    )
    .toBe("completed");
  let card: Schema["Card"];
  await expect
    .poll(async () => {
      const catalog: Schema["CatalogPage"] = await (
        await page.request.get(`${origin}/api/catalog?libraryId=${library.id}`)
      ).json();
      card = catalog.items[0];
      return card?.metadata?.source;
    })
    .toBe(sources[sources.length - 1]);
  return card!;
}
async function play(page: Page) {
  await page.getByRole("button", { name: "网页播放", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await expect
    .poll(() =>
      dialog
        .locator("video")
        .evaluate(
          (v) =>
            (v as HTMLVideoElement).getVideoPlaybackQuality().totalVideoFrames,
        ),
    )
    .toBeGreaterThan(0);
  await dialog.getByRole("button", { name: "关闭播放器", exact: true }).click();
}
for (const source of ["tmdb", "tvdb"] as const) {
  test(`${source} 当前契约：电影电视剧图片、人物作品、角色与真实播放`, async ({
    page,
  }, info) => {
    const movie = await add(page, "movie", [source]);
    const series = await add(page, "series", [source]);
    await page.goto(`${origin}/media/${movie.id}`);
    await expect(page.getByRole("heading", { level: 1 })).toHaveText(
      source === "tmdb" ? "降临" : "TVDB 降临",
    );
    await expect(page.getByRole("link", { name: /非导演编剧/ })).toHaveCount(0);
    await expect
      .poll(() =>
        page
          .locator("main img")
          .evaluateAll(
            (imgs) =>
              imgs.filter((i) => (i as HTMLImageElement).naturalWidth > 0)
                .length,
          ),
      )
      .toBeGreaterThanOrEqual(3);
    await page
      .getByRole("link", {
        name:
          source === "tmdb" ? "测试主演 · 语言学家" : "TVDB 主演 · 语言学家",
        exact: true,
      })
      .click();
    await expect(page.getByTestId("media-card")).toHaveCount(2);
    await expect
      .poll(() =>
        page
          .locator("main img")
          .first()
          .evaluate((i) => (i as HTMLImageElement).naturalWidth),
      )
      .toBeGreaterThan(0);
    await page.goto(`${origin}/series/${series.id}`);
    await expect(page.getByRole("heading", { level: 1 })).toHaveText(
      source === "tmdb" ? "暗黑" : "TVDB 暗黑",
    );
    await page.setViewportSize({ width: 390, height: 844 });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    await page.screenshot({
      path: info.outputPath(`${source}-series-mobile.png`),
    });
    await page.getByTestId("media-card").getByRole("link").click();
    await play(page);
  });
}
test("上游故障回退与可选资料失败不阻断真实播放", async ({ page }) => {
  await page.request.post("http://127.0.0.1:18094/control?mode=error");
  await page.request.post("http://127.0.0.1:18092/control?mode=credits-error");
  const movie = await add(page, "movie", ["tvdb", "tmdb"]);
  await page.goto(`${origin}/media/${movie.id}`);
  await expect(
    page.getByText("演职员资料尚未完整，可稍后刷新查看。"),
  ).toBeVisible();
  await play(page);
});
test("照片与横图下载失败保留人物作品和本地播放", async ({ page }) => {
  await page.request.post("http://127.0.0.1:18092/control?mode=photo-error");
  const movie = await add(page, "movie", ["tmdb"]);
  await page.goto(`${origin}/media/${movie.id}`);
  await page
    .getByRole("link", { name: "测试主演 · 语言学家", exact: true })
    .click();
  await expect(page.getByTestId("media-card")).toHaveCount(1);
  await expect(page.locator("main img").first()).toBeHidden();
  await page.request.post(
    "http://127.0.0.1:18092/control?mode=artwork-image-error",
  );
  await page.getByTestId("media-card").getByRole("link").click();
  await play(page);
});

test("MetaTube 按演员名字查询资料和壁纸，无需本地影片", async ({
  page,
}, info) => {
  await page.goto(origin + "/actors");
  await expect(page.getByText(/尚未配置 MetaTube/)).toBeVisible();
  expect(
    (
      await page.request.patch(origin + "/api/settings/metadata/metatube", {
        data: {
          revision: 1,
          url: "http://127.0.0.1:18095",
          credential: "isolated-browser-metatube-token",
        },
      })
    ).status(),
  ).toBe(200);
  await page.reload();
  await page.getByLabel("演员名字").fill("MetaTube 演员");
  await page.getByRole("button", { name: "查询演员", exact: true }).click();
  await expect(page.getByText("找到 2 个候选，请选择演员")).toBeVisible();
  await expect(page.getByRole("region", { name: "演员资料" })).toHaveCount(0);
  await page
    .getByRole("button", { name: /MetaTube 演员.*exampleactor/ })
    .click();
  await expect(page.getByText("按名字查询的演员简介")).toBeVisible();
  await expect(page.getByText("1995-01-02", { exact: true })).toBeVisible();
  await expect(page.getByText("170 cm")).toBeVisible();
  const profile = page.getByRole("region", { name: "演员资料" });
  const wallpaper = profile.getByAltText("MetaTube 演员壁纸 1");
  await wallpaper.scrollIntoViewIfNeeded();
  await expect
    .poll(() =>
      wallpaper.evaluate((image) => (image as HTMLImageElement).naturalWidth),
    )
    .toBeGreaterThan(0);
  await expect(profile.getByRole("link", { name: "下载图片" })).toHaveCount(2);
  const imageLink = await profile
    .getByRole("link", { name: /查看大图 1/ })
    .getAttribute("href");
  expect(imageLink).toMatch(/^\/api\/metadata\/actor-images\//);
  const imageResponse = await page.request.get(origin + imageLink);
  expect(imageResponse.status()).toBe(200);
  expect(imageResponse.headers()["content-type"]).toBe("image/png");
  await page.evaluate(() => window.scrollTo(0, 0));
  await page.screenshot({
    path: info.outputPath("actors-desktop.png"),
    fullPage: true,
  });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.evaluate(() => window.scrollTo(0, 0));
  await expect
    .poll(() =>
      page.evaluate(() => document.documentElement.scrollWidth <= innerWidth),
    )
    .toBe(true);
  await page.screenshot({
    path: info.outputPath("actors-mobile.png"),
    fullPage: true,
  });
  await page.getByLabel("演员名字").fill("不存在的演员");
  await page.getByRole("button", { name: "查询演员", exact: true }).click();
  await expect(
    page.getByText("未找到演员，请尝试其他名字或别名。"),
  ).toBeVisible();
  await expect(profile).toHaveCount(0);
  await page.goBack();
  await expect(page.getByText("按名字查询的演员简介")).toBeVisible();
});
