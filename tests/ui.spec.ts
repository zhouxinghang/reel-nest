import { expect, test } from "@playwright/test";
import { copyFileSync, mkdirSync, utimesSync } from "node:fs";
import { join } from "node:path";
import { randomUUID } from "node:crypto";

test("Emby 风格媒体视图：分类、排序、续播卡片、键盘与移动端导航", async ({
  page,
}, testInfo) => {
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.goto("/login");
  await page.screenshot({
    path: testInfo.outputPath("login-desktop.png"),
    fullPage: true,
    animations: "disabled",
  });
  await page.getByLabel("家庭共享账号", { exact: true }).fill("family");
  await page.getByLabel("密码", { exact: true }).fill("browser-test-password");
  await page.getByRole("button", { name: "进入媒体库" }).click();
  await expect(
    page.getByRole("heading", { name: "媒体库", exact: true }),
  ).toBeVisible();

  // Real scan/progress APIs and isolated playable files, not mocked app state.
  const root = join(
    process.env.REELNEST_TEST_ROOT!,
    "media",
    `ui-${randomUUID()}`,
  );
  mkdirSync(root, { recursive: true });
  const names = [
    "UI Alpha.webm",
    "UI Bravo.webm",
    "UI Charlie.webm",
    "UI Delta.webm",
    "UI Echo.webm",
    "UI Zulu.webm",
  ];
  for (const [index, name] of names.entries()) {
    const path = join(root, name);
    copyFileSync("tests/fixtures/playback.webm", path);
    utimesSync(path, 1700000000 + index, 1700000000 + index);
  }
  const added = await page.request.post("/api/directories", {
    data: { path: root, kind: "movie" },
  });
  expect(added.status()).toBe(201);
  const directory = await added.json();
  await expect
    .poll(
      async () =>
        (
          await (await page.request.get("/api/directories")).json()
        ).directories.find((item: { id: string }) => item.id === directory.id)
          ?.scanStatus,
    )
    .toBe("completed");
  const items = (await (await page.request.get("/api/library")).json()).items;
  const resume = items.find(
    (item: { filename: string }) => item.filename === names[0],
  );
  const completed = items.find(
    (item: { filename: string }) => item.filename === names[1],
  );
  expect(
    (
      await page.request.post(`/api/media/${resume.id}/progress`, {
        data: { position: 18, duration: 60, completed: false },
      })
    ).ok(),
  ).toBe(true);
  expect(
    (
      await page.request.post(`/api/media/${completed.id}/progress`, {
        data: { position: 60, duration: 60, completed: true },
      })
    ).ok(),
  ).toBe(true);

  await page.goto("/library");
  const continuing = page.getByRole("region", { name: "继续观看" });
  await expect(
    continuing.getByRole("link", { name: /UI Alpha/ }),
  ).toHaveAttribute("href", `/media/${resume.id}?play=1`);
  await expect(
    continuing.getByRole("progressbar", { name: "UI Alpha.webm 观看进度" }),
  ).toHaveAttribute("value", "18");
  await expect(continuing.getByRole("link", { name: /UI Bravo/ })).toHaveCount(
    0,
  );
  await expect(
    page.getByLabel("已看完", { exact: true }).first(),
  ).toBeVisible();
  await page.screenshot({
    path: testInfo.outputPath("library-desktop.png"),
    fullPage: true,
    animations: "disabled",
  });

  await page.getByRole("button", { name: "浏览电影", exact: true }).click();
  await expect(page).toHaveURL(/type=movie/);
  await expect(
    page
      .getByRole("navigation", { name: "主导航" })
      .getByRole("link", { name: "电影", exact: true }),
  ).toHaveAttribute("aria-current", "page");
  await page.getByLabel("搜索文件名").fill("UI ");
  const cards = page.getByRole("region", { name: "媒体收藏" });
  await expect(
    cards.getByRole("heading", { level: 2 }).filter({ hasText: /^UI / }),
  ).toHaveCount(6);
  await expect(cards.locator(".media-card h2").first()).toHaveText(
    "UI Zulu.webm",
  );
  await page.getByLabel("排序方式").selectOption("title");
  await expect(cards.locator(".media-card h2")).toHaveText(names);
  await page.screenshot({
    path: testInfo.outputPath("library-filtered.png"),
    fullPage: true,
    animations: "disabled",
  });
  await page.getByRole("button", { name: "列表视图", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "列表视图", exact: true }),
  ).toHaveAttribute("aria-pressed", "true");
  await expect(cards.locator(".poster-grid")).toHaveClass(/is-list/);
  await page.screenshot({
    path: testInfo.outputPath("library-list.png"),
    fullPage: true,
    animations: "disabled",
  });
  await page.getByRole("button", { name: "海报视图", exact: true }).click();

  await page.setViewportSize({ width: 390, height: 844 });
  // Reload after the desktop/mobile compositor resize and verify URL state.
  await page.reload();
  await expect(
    page.getByRole("button", { name: "电影", exact: true }),
  ).toHaveAttribute("aria-pressed", "true");
  await page.getByLabel("搜索文件名").fill("UI ");
  await page.getByLabel("排序方式").selectOption("title");
  await expect(cards.locator(".media-card h2")).toHaveText(names);
  await expect(
    page.getByRole("navigation", { name: "主导航" }),
  ).not.toBeVisible();
  await page.screenshot({
    path: testInfo.outputPath("library-mobile.png"),
    fullPage: true,
    animations: "disabled",
  });
  await page.getByRole("button", { name: "打开导航", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "收起导航", exact: true }),
  ).toHaveAttribute("aria-expanded", "true");
  await expect(
    page
      .getByRole("navigation", { name: "主导航" })
      .getByRole("link", { name: "媒体库", exact: true }),
  ).toBeFocused();
  await page.keyboard.press("Escape");
  await expect(
    page.getByRole("button", { name: "打开导航", exact: true }),
  ).toBeFocused();
  await page.getByRole("button", { name: "打开导航", exact: true }).click();
  await page
    .getByRole("navigation", { name: "主导航" })
    .getByRole("link", { name: "媒体目录", exact: true })
    .click();
  await expect(
    page.getByRole("heading", { name: "媒体目录", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "打开导航", exact: true }),
  ).toHaveAttribute("aria-expanded", "false");
  await page.getByRole("link", { name: "搜索媒体", exact: true }).click();
  await expect(page.getByLabel("搜索文件名")).toBeFocused();
  await page.getByLabel("搜索文件名").fill("UI not found");
  await expect(
    page.getByRole("heading", { name: "没有找到对应的媒体" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "查看全部媒体", exact: true }).click();
  await page.setViewportSize({ width: 320, height: 900 });
  await page.getByRole("button", { name: "未匹配媒体", exact: true }).click();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
  await page.getByRole("button", { name: "全部", exact: true }).click();
  await page.getByLabel("搜索文件名").fill("UI ");
  for (const width of [320, 390, 768, 1024, 1440]) {
    await page.setViewportSize({ width, height: 900 });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
      `no horizontal overflow at ${width}px`,
    ).toBe(true);
  }
  await page
    .getByRole("region", { name: "媒体收藏" })
    .getByRole("link")
    .filter({ has: page.getByRole("heading", { name: names[0], exact: true }) })
    .click();
  await expect(
    page.getByRole("heading", { name: names[0], exact: true }),
  ).toBeVisible();
  await page.screenshot({
    path: testInfo.outputPath("detail-desktop.png"),
    fullPage: true,
    animations: "disabled",
  });
  expect(errors).toEqual([]);
});

test("Emby 详情页：全宽布局、续播与从头播放、图标操作、季集和窄屏", async ({
  page,
}, testInfo) => {
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.goto("/login");
  await page.getByLabel("家庭共享账号", { exact: true }).fill("family");
  await page.getByLabel("密码", { exact: true }).fill("browser-test-password");
  await page.getByRole("button", { name: "进入媒体库" }).click();
  await expect(
    page.getByRole("heading", { name: "媒体库", exact: true }),
  ).toBeVisible();

  const root = join(
    process.env.REELNEST_TEST_ROOT!,
    "media",
    `detail-${randomUUID()}`,
  );
  const filename =
    "未匹配的家庭电影.with-a-very-long-original-filename.2026.webm";
  async function addFiles(folder: string, kind: string, names: string[]) {
    const path = join(root, folder);
    mkdirSync(path, { recursive: true });
    for (const name of names)
      copyFileSync("tests/fixtures/playback.webm", join(path, name));
    const response = await page.request.post("/api/directories", {
      data: { path, kind },
    });
    expect(response.status()).toBe(201);
    const directory = await response.json();
    await expect
      .poll(
        async () =>
          (
            await (await page.request.get("/api/directories")).json()
          ).directories.find((item: { id: string }) => item.id === directory.id)
            ?.scanStatus,
      )
      .toBe("completed");
    return (await (await page.request.get("/api/library")).json()).items.filter(
      (item: { directoryId: string }) => item.directoryId === directory.id,
    );
  }
  const [file] = await addFiles("movies", "movie", [filename]);
  expect(
    (
      await page.request.post(`/api/media/${file.id}/progress`, {
        data: { position: 18, duration: 60, completed: false },
      })
    ).ok(),
  ).toBe(true);
  await page.goto(`/media/${file.id}`);
  const hero = page.getByRole("region", { name: "影片详情", exact: true });
  await expect(hero.getByRole("heading", { level: 1 })).toHaveText(filename);
  await expect(
    page.getByRole("navigation", { name: "主导航" }),
  ).not.toBeVisible();
  await expect(
    page.getByRole("progressbar", { name: "观看进度", exact: true }),
  ).toHaveAttribute("value", "18");
  await expect(page.getByText("从 0:18 继续 · 剩余 0:42")).toBeVisible();
  const info = page.getByRole("region", { name: "媒体信息", exact: true });
  await expect(info.getByText(file.path, { exact: true })).toBeVisible();
  const heroBox = (await hero.boundingBox())!;
  expect(heroBox.x).toBeLessThan(80);
  expect((await info.boundingBox())!.y).toBeGreaterThanOrEqual(
    heroBox.y + heroBox.height,
  );
  await page.screenshot({
    path: testInfo.outputPath("emby-detail-desktop.png"),
    fullPage: true,
  });

  const edit = page.getByRole("button", {
    name: "手动匹配影片资料",
    exact: true,
  });
  await edit.click();
  await expect(
    page.getByRole("dialog", { name: "手动匹配影片资料", exact: true }),
  ).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(edit).toBeFocused();
  const remove = page.getByRole("button", { name: "删除源文件", exact: true });
  await remove.click();
  await expect(page.getByRole("dialog")).toContainText("本系统无法恢复");
  await page.getByRole("button", { name: "取消", exact: true }).click();
  await expect(remove).toBeFocused();

  await page.getByRole("button", { name: "网页播放", exact: true }).click();
  const player = page.getByRole("dialog", { name: "网页播放器", exact: true });
  await expect(player).toBeVisible();
  await expect
    .poll(() =>
      page.locator("video").evaluate((v: HTMLVideoElement) => v.currentTime),
    )
    .toBeGreaterThan(18);
  await expect(
    page.getByRole("button", { name: "暂停", exact: true }),
  ).toBeVisible();
  const box = (await player.boundingBox())!;
  expect(box).toEqual({ x: 0, y: 0, width: 1440, height: 1000 });
  await page.screenshot({
    path: testInfo.outputPath("web-fullscreen-desktop.png"),
  });
  await page.keyboard.press("Escape");
  await expect(player).not.toBeVisible();
  await expect(
    page.getByRole("button", { name: "网页播放", exact: true }),
  ).toBeFocused();
  expect(
    (await (await page.request.get(`/api/media/${file.id}`)).json()).progress
      .position,
  ).toBeGreaterThan(18);

  await page.getByRole("button", { name: "从头开始", exact: true }).click();
  await expect(player).toBeVisible();
  await expect(
    page.getByRole("button", { name: "暂停", exact: true }),
  ).toBeVisible();
  expect(
    await page
      .locator("video")
      .evaluate((v: HTMLVideoElement) => v.currentTime),
  ).toBeLessThan(3);
  await page.setViewportSize({ width: 390, height: 844 });
  await expect
    .poll(() => player.boundingBox())
    .toEqual({ x: 0, y: 0, width: 390, height: 844 });
  expect(
    await player.evaluate(
      (element) => element.scrollWidth <= element.clientWidth,
    ),
  ).toBe(true);
  const closeBox = (await page
    .getByRole("button", { name: "关闭播放器", exact: true })
    .boundingBox())!;
  expect(closeBox.x + closeBox.width).toBeLessThanOrEqual(390);
  await page.screenshot({
    path: testInfo.outputPath("web-fullscreen-mobile.png"),
  });
  await page.getByRole("button", { name: "关闭播放器", exact: true }).click();
  await expect(player).not.toBeVisible();
  await expect(page.locator("video")).toHaveCount(0);
  await expect(page.locator("html")).not.toHaveClass(/web-player-open/);

  await page.getByRole("button", { name: "复制链接", exact: true }).click();
  await expect(
    page.getByRole("link", { name: "用 IINA 打开", exact: true }),
  ).toHaveAttribute("href", /^iina:\/\/open\?url=/);
  await expect(
    page.getByRole("link", { name: "用 PotPlayer 打开", exact: true }),
  ).toHaveAttribute("href", /^potplayer:\/\//);
  await expect(
    page.getByRole("link", { name: "用 VLC 打开", exact: true }),
  ).toHaveAttribute("href", /^vlc:\/\//);
  await expect(
    page.getByText("持有链接者可读取此视频", { exact: false }),
  ).toBeVisible();

  await page.goto(`/media/${file.id}`);
  for (const width of [320, 390, 520, 768, 1024, 1440]) {
    await page.setViewportSize({ width, height: 900 });
    await page.reload();
    await expect(hero).toBeVisible();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
      `detail overflow at ${width}`,
    ).toBe(true);
    if (width === 390)
      await page.screenshot({
        path: testInfo.outputPath("emby-detail-mobile.png"),
        fullPage: true,
      });
  }
  await page.getByRole("button", { name: "打开导航", exact: true }).click();
  await expect(
    page
      .getByRole("navigation", { name: "主导航" })
      .getByRole("link", { name: "媒体库", exact: true }),
  ).toBeFocused();
  await page.keyboard.press("Escape");
  await expect(
    page.getByRole("button", { name: "打开导航", exact: true }),
  ).toBeFocused();
  await page.getByRole("button", { name: "打开导航", exact: true }).click();
  await page
    .getByRole("button", { name: "关闭导航", exact: true })
    .click({ position: { x: 500, y: 100 } });
  await expect(
    page.getByRole("navigation", { name: "主导航" }),
  ).not.toBeVisible();

  const episodes = await addFiles("series", "series", [
    "星际旅行.S02E02.webm",
    "星际旅行.S02E10.webm",
    "星际旅行.S10E01.webm",
  ]);
  const episode = episodes.find(
    (item: { episode: { seasonNumber: number; episodeNumber: number } }) =>
      item.episode.seasonNumber === 2 && item.episode.episodeNumber === 2,
  );
  await page.goto(`/series/${episode.episode.seriesId}?season=2`);
  await expect(
    hero.getByRole("heading", { name: "星际旅行", exact: true }),
  ).toBeVisible();
  await expect(page.getByRole("heading", { name: /^第 \d+ 集$/ })).toHaveText([
    "第 2 集",
    "第 10 集",
  ]);
  await page.screenshot({
    path: testInfo.outputPath("emby-series-desktop.png"),
    fullPage: true,
  });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.reload();
  await expect(hero).toBeVisible();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
  await page.screenshot({
    path: testInfo.outputPath("emby-series-mobile.png"),
    fullPage: true,
  });
  await page.getByRole("link", { name: /星际旅行.S02E02.webm/ }).click();
  await expect(hero).toContainText("第 2 季 · 第 2 集");
  await expect(
    page.getByRole("link", { name: "返回第 2 季", exact: true }),
  ).toHaveAttribute("href", `/series/${episode.episode.seriesId}?season=2`);
  expect(errors).toEqual([]);
});
