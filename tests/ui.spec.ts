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
