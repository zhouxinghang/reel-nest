import { expect, test, type Browser, type Page } from "@playwright/test";
import { spawn, type ChildProcess } from "node:child_process";
import { mkdirSync, mkdtempSync, realpathSync, writeFileSync } from "node:fs";
import { join, resolve } from "node:path";
import type { components } from "../../web/src/generated/api";

type Schema = components["schemas"];
const origin = "http://127.0.0.1:18096";
let child: ChildProcess;
let movieLibrary: Schema["Library"];
let otherLibrary: Schema["Library"];
let seriesId: string;
let firstFile: Schema["Card"];
let moviesPath: string;

async function login(page: Page, path = "/") {
  await page.goto(origin + path);
  await expect(page.getByRole("heading", { name: "欢迎回家" })).toBeVisible();
  await page.getByLabel("家庭共享账号").fill("family");
  await page.getByLabel("密码", { exact: true }).fill("browser-test-password");
  await page.getByRole("button", { name: "进入媒体库 →" }).click();
}
async function signedIn(browser: Browser) {
  const context = await browser.newContext({ baseURL: origin });
  const result = await context.request.post("/api/login", {
    data: { username: "family", password: "browser-test-password" },
  });
  expect(result.status()).toBe(200);
  return context;
}
test.beforeAll(async ({ browser }) => {
  const root = process.env.REELNEST_TEST_ROOT!;
  const run = mkdtempSync(join(root, "runtime-ui-"));
  const media = join(run, "media");
  moviesPath = join(media, "movies");
  for (const path of [
    moviesPath,
    join(media, "other"),
    join(media, "empty"),
    join(media, "tv", "家庭节目"),
  ])
    mkdirSync(path, { recursive: true });
  for (let i = 1; i <= 53; i++)
    writeFileSync(
      join(moviesPath, `电影${String(i).padStart(3, "0")}.webm`),
      "isolated catalog fixture, not codec proof",
    );
  for (let i = 1; i <= 12; i++)
    writeFileSync(
      join(media, "other", `短片${String(i).padStart(3, "0")}.webm`),
      "fixture",
    );
  for (let i = 1; i <= 53; i++)
    writeFileSync(
      join(media, "tv", "家庭节目", `S01E${String(i).padStart(2, "0")}.webm`),
      "fixture",
    );
  writeFileSync(join(media, "tv", "家庭节目", "S02E01.webm"), "fixture");
  child = spawn(join(root, "reelnest-runtime"), [], {
    env: {
      ...process.env,
      DATA_DIR: join(run, "data"),
      MEDIA_ROOTS: media,
      RUNTIME_WEB_DIR: resolve(".reelnest-test/runtime-web"),
    },
    stdio: "inherit",
  });
  await expect
    .poll(async () => {
      if (child.exitCode !== null) throw new Error("runtime exited");
      try {
        return (await fetch(`${origin}/readyz`)).status;
      } catch {
        return 0;
      }
    })
    .toBe(200);
  const context = await signedIn(browser);
  try {
    for (const [name, path, kind] of [
      ["分页电影", moviesPath, "movie"],
      ["短片媒体库", join(media, "other"), "movie"],
      ["空媒体库", join(media, "empty"), "movie"],
      ["电视剧媒体库", join(media, "tv"), "series"],
    ] as const) {
      const created = await context.request.post("/api/libraries", {
        data: { name, path, kind, sources: [] },
      });
      expect(created.status()).toBe(201);
      const library: Schema["Library"] = await created.json();
      await expect
        .poll(async () => {
          const scan: Schema["Scan"] = await (
            await context.request.get(`/api/scans/${library.latestScanId}`)
          ).json();
          return scan.state;
        })
        .toBe("completed");
      if (name === "分页电影") movieLibrary = library;
      if (name === "短片媒体库") otherLibrary = library;
      if (kind === "series") {
        const catalog: Schema["CatalogPage"] = await (
          await context.request.get(`/api/catalog?libraryId=${library.id}`)
        ).json();
        expect(catalog.items[0].entryType).toBe("series");
        seriesId = catalog.items[0].id;
      }
    }
    const catalog: Schema["CatalogPage"] = await (
      await context.request.get(`/api/catalog?libraryId=${movieLibrary.id}`)
    ).json();
    firstFile = catalog.items[0];
    await expect
      .poll(async () => {
        const status: Schema["MetadataStatus"] = await (
          await context.request.get("/api/metadata/status")
        ).json();
        return status.queued + status.running;
      })
      .toBe(0);
  } finally {
    await context.close();
  }
});
test.afterAll(async () => {
  if (!child || child.exitCode !== null) return;
  await new Promise<void>((done) => {
    child.once("exit", () => done());
    child.kill("SIGTERM");
  });
});

test("新 UI 登录、首页有界读取、URL 分页/搜索/视图与前进后退", async ({
  page,
}) => {
  const reads: string[] = [];
  page.on("request", (request) => {
    if (request.method() === "GET" && request.url().includes("/api/"))
      reads.push(new URL(request.url()).pathname);
  });
  await login(page);
  await expect(
    page.getByRole("heading", { name: "好故事，在家一起看。" }),
  ).toBeVisible();
  await expect(
    page.getByRole("region", { name: "空媒体库", exact: true }),
  ).toContainText("没有符合条件的内容");
  await expect(
    page.getByRole("status").filter({ hasText: /影片资料.*(空闲|需稍后重试)/ }),
  ).toBeVisible();
  // Give the initial status notification time to settle: no full-catalog reload.
  await page.waitForTimeout(1700);
  expect(reads.filter((p) => p === "/api/home")).toHaveLength(1);
  expect(reads.filter((p) => p === "/api/metadata/status")).toHaveLength(1);
  expect(reads.filter((p) => p === "/api/catalog")).toHaveLength(0);
  await page.getByRole("link", { name: "分页电影 →" }).click();
  await expect(page.getByTestId("media-card")).toHaveCount(50);
  const firstPage = await page
    .getByTestId("media-card")
    .locator("h3")
    .allTextContents();
  await page.getByRole("link", { name: "下一页", exact: true }).click();
  await expect(page.getByTestId("media-card")).toHaveCount(3);
  const pageTwo = page.url();
  expect(new URL(pageTwo).searchParams.get("cursor")).toBeTruthy();
  await page.reload();
  await expect(page.getByTestId("media-card")).toHaveCount(3);
  await page.goBack();
  await expect(page.getByTestId("media-card")).toHaveCount(50);
  expect(
    await page.getByTestId("media-card").locator("h3").allTextContents(),
  ).toEqual(firstPage);
  await page.goForward();
  await expect(page.getByTestId("media-card")).toHaveCount(3);
  await page.getByLabel("搜索", { exact: true }).fill("电影001");
  await page.getByRole("button", { name: "搜索", exact: true }).click();
  await expect(page.getByTestId("media-card")).toHaveCount(1);
  expect(new URL(page.url()).searchParams.has("cursor")).toBe(false);
  await page.getByLabel("视图", { exact: true }).selectOption("list");
  await page.getByLabel("排序", { exact: true }).selectOption("recent");
  await page.reload();
  await expect(page.getByLabel("搜索", { exact: true })).toHaveValue("电影001");
  await expect(page.getByLabel("视图", { exact: true })).toHaveValue("list");
  await expect(page.getByLabel("排序", { exact: true })).toHaveValue("recent");
  await expect(page.locator("[data-layout=list]")).toBeVisible();
  await page.getByTestId("media-card").getByRole("link").click();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("电影001");
  await expect(
    page.getByText(join(realpathSync(moviesPath), "电影001.webm"), {
      exact: true,
    }),
  ).toBeVisible();
});

test("全局分组独立分页、真实收藏写入及失效 cursor 恢复", async ({
  browser,
}) => {
  const context = await signedIn(browser);
  try {
    const page = await context.newPage();
    await page.goto(origin + "/library?category=movie");
    const movies = page.getByRole("region", { name: "分页电影", exact: true });
    const other = page.getByRole("region", { name: "短片媒体库", exact: true });
    await expect(movies.getByTestId("media-card")).toHaveCount(10);
    await expect(other.getByTestId("media-card")).toHaveCount(10);
    const unchanged = await other
      .getByTestId("media-card")
      .locator("h3")
      .allTextContents();
    await movies.getByRole("link", { name: "下一页", exact: true }).click();
    await expect(movies.getByTestId("media-card").first()).toContainText(
      "电影011",
    );
    expect(
      await other.getByTestId("media-card").locator("h3").allTextContents(),
    ).toEqual(unchanged);
    expect(
      new URL(page.url()).searchParams.has(`group.${movieLibrary.id}`),
    ).toBe(true);
    expect(
      new URL(page.url()).searchParams.has(`group.${otherLibrary.id}`),
    ).toBe(false);
    const selected = movies.getByTestId("media-card").first();
    const title = await selected.locator("h3").innerText();
    await selected
      .getByRole("button", { name: `收藏 ${title}`, exact: true })
      .click();
    await expect(
      selected.getByRole("button", { name: `取消收藏 ${title}`, exact: true }),
    ).toHaveAttribute("aria-pressed", "true");
    await page
      .getByRole("navigation", { name: "主导航" })
      .getByRole("link", { name: "收藏", exact: true })
      .click();
    await expect(page.getByTestId("media-card")).toHaveCount(1);
    await expect(page.getByTestId("media-card")).toContainText(title);
    await page
      .getByRole("button", { name: `取消收藏 ${title}`, exact: true })
      .click();
    await expect(page.getByTestId("media-card")).toHaveCount(0);
    // A scan really changes catalog revision; an old keyset cannot be reused.
    await page.goto(`${origin}/library?library=${movieLibrary.id}`);
    await page.getByRole("link", { name: "下一页", exact: true }).click();
    await expect(page.getByTestId("media-card")).toHaveCount(3);
    const scan: Schema["Scan"] = await (
      await context.request.post(`/api/libraries/${movieLibrary.id}/scans`, {
        data: {},
      })
    ).json();
    await expect
      .poll(
        async () =>
          (
            (await (
              await context.request.get(`/api/scans/${scan.id}`)
            ).json()) as Schema["Scan"]
          ).state,
      )
      .toBe("completed");
    await page.reload();
    await expect(page.getByTestId("media-card")).toHaveCount(50);
    expect(new URL(page.url()).searchParams.has("cursor")).toBe(false);
    await expect(page.getByRole("alert")).toHaveCount(0);
    await page.goto(origin + "/library?category=movie");
    await movies.getByRole("link", { name: "下一页", exact: true }).click();
    await expect(movies.getByTestId("media-card").first()).toContainText(
      "电影011",
    );
    const again: Schema["Scan"] = await (
      await context.request.post(`/api/libraries/${movieLibrary.id}/scans`, {
        data: {},
      })
    ).json();
    await expect
      .poll(
        async () =>
          (
            (await (
              await context.request.get(`/api/scans/${again.id}`)
            ).json()) as Schema["Scan"]
          ).state,
      )
      .toBe("completed");
    await page.reload();
    await expect(movies.getByTestId("media-card").first()).toContainText(
      "电影001",
    );
    expect(
      new URL(page.url()).searchParams.has(`group.${movieLibrary.id}`),
    ).toBe(false);
    await expect(page.getByRole("alert")).toHaveCount(0);
  } finally {
    await context.close();
  }
});

test("电视剧季/单集分页使用 URL、刷新与返回保持，未加载全季集", async ({
  browser,
}) => {
  const context = await signedIn(browser);
  try {
    const page = await context.newPage();
    await page.goto(`${origin}/series/${seriesId}?season=1`);
    await expect(page.getByTestId("media-card")).toHaveCount(50);
    await expect(page.getByText("2 季 · 54 集 · 54 个文件版本")).toBeVisible();
    await page.getByRole("link", { name: "下一页", exact: true }).click();
    await expect(page.getByTestId("media-card")).toHaveCount(3);
    await page.reload();
    await expect(page.getByTestId("media-card")).toHaveCount(3);
    await page.getByRole("button", { name: "第 2 季", exact: true }).click();
    await expect(page.getByTestId("media-card")).toHaveCount(1);
    expect(new URL(page.url()).searchParams.get("season")).toBe("2");
    expect(new URL(page.url()).searchParams.has("episodes")).toBe(false);
    await page.goBack();
    await expect(page.getByTestId("media-card")).toHaveCount(3);
    await expect(
      page.getByRole("button", { name: "第 1 季", exact: true }),
    ).toHaveAttribute("aria-pressed", "true");
  } finally {
    await context.close();
  }
});

test("真实设置 CAS 冲突保留草稿、不自动重试，注销后 401 回到原 URL", async ({
  browser,
}) => {
  const context = await signedIn(browser);
  const other = await signedIn(browser);
  try {
    const page = await context.newPage();
    await page.goto(origin + "/settings/metadata");
    const form = page.getByRole("form", { name: "MetaTube 设置" });
    await form.getByLabel("服务地址").fill("http://127.0.0.1:9/draft");
    const settings: Schema["Settings"] = await (
      await other.request.get("/api/settings/metadata")
    ).json();
    const initial = settings.sources.find((s) => s.source === "metatube")!;
    expect(
      (
        await other.request.patch("/api/settings/metadata/metatube", {
          data: { revision: initial.revision, url: "http://127.0.0.1:9/other" },
        })
      ).status(),
    ).toBe(200);
    await form
      .getByRole("button", { name: "保存 MetaTube", exact: true })
      .click();
    await expect(form.getByText(/其他会话修改了配置/)).toBeVisible();
    await expect(form.getByLabel("服务地址")).toHaveValue(
      "http://127.0.0.1:9/draft",
    );
    await expect(
      form.getByRole("button", { name: "保存 MetaTube", exact: true }),
    ).toBeDisabled();
    await form.getByRole("button", { name: "读取最新版本，保留草稿" }).click();
    await expect(form.getByText(/草稿未变/)).toBeVisible();
    await form.getByLabel("明确清除已保存凭据").check();
    await form
      .getByRole("button", { name: "保存 MetaTube", exact: true })
      .click();
    await expect(form.getByText(/设置已保存/)).toBeVisible();
    const saved: Schema["Settings"] = await (
      await other.request.get("/api/settings/metadata")
    ).json();
    expect(saved.sources.find((s) => s.source === "metatube")?.url).toBe(
      "http://127.0.0.1:9/draft/v1",
    );
    await context.request.post("/api/logout", { data: {} });
    await page.goto(
      `${origin}/library?library=${movieLibrary.id}&layout=list&q=电影001`,
    );
    await expect(page.getByRole("heading", { name: "欢迎回家" })).toBeVisible();
    expect(new URL(page.url()).pathname).toBe("/login");
    await page.getByLabel("家庭共享账号").fill("family");
    await page
      .getByLabel("密码", { exact: true })
      .fill("browser-test-password");
    await page.getByRole("button", { name: "进入媒体库 →" }).click();
    await expect(page.getByTestId("media-card")).toHaveCount(1);
    await expect(page.getByLabel("视图", { exact: true })).toHaveValue("list");
  } finally {
    await context.close();
    await other.close();
  }
});

test("390px 与键盘导航、卡片可访问且无水平溢出", async ({
  browser,
}, testInfo) => {
  const context = await browser.newContext({
    baseURL: origin,
    viewport: { width: 390, height: 844 },
  });
  try {
    const page = await context.newPage();
    await login(page, `/library?library=${movieLibrary.id}`);
    await expect(page.getByTestId("media-card")).toHaveCount(50);
    const menu = page.getByRole("button", { name: "打开导航", exact: true });
    await menu.focus();
    await page.keyboard.press("Enter");
    await expect(
      page
        .getByRole("navigation", { name: "主导航" })
        .getByRole("link", { name: "首页", exact: true }),
    ).toBeFocused();
    await page.keyboard.press("Escape");
    await expect(menu).toBeFocused();
    await expect(menu).toHaveAttribute("aria-expanded", "false");
    await expect
      .poll(() =>
        page.evaluate(
          () => document.documentElement.scrollWidth <= window.innerWidth,
        ),
      )
      .toBe(true);
    const link = page.getByTestId("media-card").first().getByRole("link");
    await link.focus();
    await page.keyboard.press("Enter");
    await expect(page.getByRole("heading", { level: 1 })).toHaveText(
      firstFile.title,
    );
    await expect
      .poll(() =>
        page.evaluate(
          () => document.documentElement.scrollWidth <= window.innerWidth,
        ),
      )
      .toBe(true);
    await page.goBack();
    await expect(page.getByTestId("media-card")).toHaveCount(50);
    await page.screenshot({
      path: testInfo.outputPath("runtime-ui-390.png"),
      fullPage: false,
    });
    await page.setViewportSize({ width: 1440, height: 1000 });
    await expect(page.getByRole("banner")).toHaveCount(1);
    await page.evaluate(
      () =>
        new Promise<void>((done) =>
          requestAnimationFrame(() => requestAnimationFrame(() => done())),
        ),
    );
    await page.screenshot({
      path: testInfo.outputPath("runtime-ui-desktop.png"),
      fullPage: false,
    });
  } finally {
    await context.close();
  }
});
