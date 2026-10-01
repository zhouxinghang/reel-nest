import { expect, test } from "@playwright/test";
import { mkdirSync, writeFileSync, renameSync } from "node:fs";
import { join, dirname } from "node:path";

test("家庭共享账号、扫描、电影与电视剧季集导航、失联恢复和退出闭环", async ({
  page,
  context,
  browser,
}) => {
  await page.goto("/library");
  await expect(page).toHaveURL(/\/login$/);
  await page.getByLabel("家庭共享账号", { exact: true }).fill("family");
  await page.getByLabel("密码", { exact: true }).fill("wrong");
  await page.getByRole("button", { name: "进入媒体库" }).click();
  await expect(page.getByRole("alert")).toHaveText("账号或密码错误");
  await page.getByLabel("密码", { exact: true }).fill("browser-test-password");
  await page.getByRole("button", { name: "进入媒体库" }).click();
  await expect(
    page.getByRole("heading", { name: "媒体库", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "媒体库还是空的", exact: true }),
  ).toBeVisible();

  const root = process.env.REELNEST_TEST_ROOT!;
  const media = join(root, "media", "browser-library");
  mkdirSync(media, { recursive: true });
  writeFileSync(
    join(media, "家庭电影 (2026).mp4"),
    "isolated browser fixture; not a playback test",
  );
  await page.getByRole("link", { name: "接入第一个媒体目录" }).click();
  await page.getByLabel("媒体目录路径").fill(media);
  await page.getByRole("button", { name: "添加并扫描" }).click();
  await expect(page.getByRole("status")).toHaveText("扫描完成");
  await page.getByRole("link", { name: "返回媒体库" }).click();
  await expect(
    page.getByRole("heading", { name: "家庭电影 (2026).mp4" }),
  ).toBeVisible();
  await page.getByRole("link", { name: "搜索媒体", exact: true }).click();
  await page.getByLabel("搜索文件名").fill("找不到的文件");
  await expect(
    page.getByRole("heading", { name: "没有找到对应的媒体" }),
  ).toBeVisible();
  await page.getByLabel("搜索文件名").fill("家庭电影");
  await page
    .getByRole("link")
    .filter({ has: page.getByRole("heading", { name: "家庭电影 (2026).mp4" }) })
    .click();
  await expect(
    page.getByRole("heading", { name: "家庭电影 (2026).mp4" }),
  ).toBeVisible();
  await expect(
    page.getByText(join(media, "家庭电影 (2026).mp4"), { exact: true }),
  ).toBeVisible();
  const detailURL = page.url();
  await page.getByRole("button", { name: "打开导航", exact: true }).click();
  await page.getByRole("link", { name: "媒体目录", exact: true }).click();
  writeFileSync(join(media, "另一个版本.webm"), "another isolated fixture");
  await page.getByRole("button", { name: "重新扫描" }).click();
  await expect(page.getByRole("status")).toHaveText("扫描完成");
  await page.getByRole("link", { name: "返回媒体库" }).click();
  await expect(
    page.getByRole("heading", { name: "另一个版本.webm" }),
  ).toBeVisible();
  await page.setViewportSize({ width: 1440, height: 960 });
  await page.reload();
  await expect(
    page.getByRole("heading", { name: "媒体库", exact: true }),
  ).toBeVisible();
  await page.screenshot({
    path: "test-results/library-desktop.png",
    fullPage: true,
  });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.reload();
  await expect(
    page.getByRole("heading", { name: "家庭电影 (2026).mp4" }),
  ).toBeVisible();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
  await page.screenshot({
    path: "test-results/library-mobile.png",
    fullPage: true,
  });
  await page.setViewportSize({ width: 1440, height: 960 });
  await page.goto("/directories");
  renameSync(media, media + "-offline");
  await page.getByRole("button", { name: "重新扫描" }).click();
  await expect(page.getByRole("status")).toHaveText("扫描失败");
  await expect(page.getByRole("alert")).toContainText("保留已有媒体记录");
  renameSync(media + "-offline", media);
  await page.getByRole("button", { name: "重新扫描" }).click();
  await expect(page.getByRole("status")).toHaveText("扫描完成");
  await page.goto(detailURL);
  await expect(
    page.getByRole("heading", { name: "家庭电影 (2026).mp4" }),
  ).toBeVisible();
  const television = join(root, "media", "television-library");
  for (const path of [
    "edition-a/星河.S2E2.1080p.mkv",
    "edition-b/星河.S2E2.1080p.mkv",
    "flat/星河.S2E10.mp4",
    "flat/星河.S10E1.mp4",
    "星河/Season 00/S00E1.mp4",
    "星河.S01E01-E02.mp4",
  ]) {
    const target = join(television, path);
    mkdirSync(dirname(target), { recursive: true });
    writeFileSync(target, "isolated series fixture; not a playback test");
  }
  await page.goto("/directories");
  await page.getByLabel("媒体目录路径").fill(television);
  await page.getByLabel("内容类型").selectOption("series");
  await page.getByRole("button", { name: "添加并扫描" }).click();
  await expect(
    page
      .getByRole("article")
      .filter({ hasText: television })
      .getByRole("status"),
  ).toHaveText("扫描完成");
  await page.getByRole("link", { name: "返回媒体库" }).click();
  await page.getByRole("button", { name: "浏览电视剧", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "家庭电影 (2026).mp4" }),
  ).toHaveCount(0);
  await page.getByLabel("搜索文件名").fill("星河.S2E10.mp4");
  await expect(
    page.getByRole("heading", { name: "星河", exact: true }),
  ).toBeVisible();
  await page.getByLabel("搜索文件名").fill("");
  await page
    .getByRole("link")
    .filter({ has: page.getByRole("heading", { name: "星河", exact: true }) })
    .click();
  await expect(
    page.getByRole("heading", { name: "星河", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("navigation", { name: "季导航" }).getByRole("link"),
  ).toHaveText(["第 0 季", "第 2 季", "第 10 季"]);
  await page.getByRole("link", { name: "第 2 季", exact: true }).click();
  await expect(page.getByRole("heading", { name: /^第 \d+ 集$/ })).toHaveText([
    "第 2 集",
    "第 10 集",
  ]);
  await page.screenshot({
    path: "test-results/series-desktop.png",
    fullPage: true,
  });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.reload();
  await expect(
    page.getByRole("heading", { name: "第 2 季", exact: true }),
  ).toBeVisible();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
  await page.screenshot({
    path: "test-results/series-mobile.png",
    fullPage: true,
  });
  await page
    .getByRole("link")
    .filter({ hasText: "edition-b/星河.S2E2.1080p.mkv" })
    .click();
  await expect(
    page.getByText(join(television, "edition-b/星河.S2E2.1080p.mkv"), {
      exact: true,
    }),
  ).toBeVisible();
  await page.getByRole("link", { name: "返回第 2 季", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "第 2 季", exact: true }),
  ).toBeVisible();
  await page.getByRole("link", { name: "第 10 季", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "第 1 集", exact: true }),
  ).toBeVisible();
  await page.goto("/library");
  await page.getByRole("button", { name: "打开导航", exact: true }).click();
  await page.getByRole("link", { name: "未匹配媒体", exact: true }).click();
  await page
    .getByRole("link")
    .filter({
      has: page.getByRole("heading", {
        name: "星河.S01E01-E02.mp4",
        exact: true,
      }),
    })
    .click();
  await expect(page.getByText(/疑似多集合一文件/)).toBeVisible();
  await page.setViewportSize({ width: 1440, height: 960 });
  const cookies = await context.cookies();
  const second = await browser.newContext();
  const otherPage = await second.newPage();
  await otherPage.goto("/login");
  await otherPage.getByLabel("家庭共享账号", { exact: true }).fill("family");
  await otherPage
    .getByLabel("密码", { exact: true })
    .fill("browser-test-password");
  await otherPage.getByRole("button", { name: "进入媒体库" }).click();
  await expect(
    otherPage.getByRole("heading", { name: "媒体库", exact: true }),
  ).toBeVisible();
  await expect(
    otherPage.getByRole("heading", { name: "家庭电影 (2026).mp4" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "退出登录" }).click();
  await expect(page.getByRole("heading", { name: "欢迎回家" })).toBeVisible();
  await context.addCookies(cookies);
  expect((await context.request.get("/api/library")).status()).toBe(401);
  expect((await second.request.get("/api/library")).status()).toBe(200);
  await second.close();
});
