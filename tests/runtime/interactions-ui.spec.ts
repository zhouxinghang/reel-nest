import { expect, test, type Page } from "@playwright/test";
import { spawn, type ChildProcess } from "node:child_process";
import {
  copyFileSync,
  existsSync,
  mkdirSync,
  mkdtempSync,
  realpathSync,
  writeFileSync,
} from "node:fs";
import { join } from "node:path";
import type { components } from "../../web/src/generated/api";
type Schema = components["schemas"];
const origin = "http://127.0.0.1:18096";
let child: ChildProcess;
let media: string;
let id: string;
let library: Schema["Library"];
async function login(page: Page, path = "/libraries") {
  await page.goto(origin + path);
  await page.getByLabel("家庭共享账号").fill("family");
  await page.getByLabel("密码", { exact: true }).fill("browser-test-password");
  await page.getByRole("button", { name: "进入媒体库 →" }).click();
  await expect(
    page.getByRole("heading", { name: "媒体库管理", exact: true }),
  ).toBeVisible();
}
test.describe.configure({ mode: "serial" });
test.beforeAll(async () => {
  const root = process.env.REELNEST_TEST_ROOT!;
  const run = mkdtempSync(join(root, "runtime-interactions-"));
  media = join(run, "media");
  mkdirSync(media);
  for (const name of ["中文 & #", "保留相邻视频"])
    copyFileSync("tests/fixtures/playback.webm", join(media, `${name}.webm`));
  writeFileSync(
    join(media, "中文 & #.zh.srt"),
    "1\n00:00:00,000 --> 00:00:59,900\n新的产品播放器字幕\n",
  );
  writeFileSync(
    join(media, "中文 & #.en.vtt"),
    "WEBVTT\n\n00:00:00.000 --> 00:00:59.900\nEnglish subtitle\n",
  );
  writeFileSync(join(media, "poster.txt"), "adjacent image placeholder");
  child = spawn(join(root, "reelnest-runtime"), [], {
    env: {
      ...process.env,
      DATA_DIR: join(run, "data"),
      MEDIA_ROOTS: media,
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
});
test.afterAll(async () => {
  if (child && child.exitCode === null)
    await new Promise<void>((done) => {
      child.once("exit", () => done());
      child.kill("SIGTERM");
    });
});

test("新 UI 完整链路：登录建库扫描、搜索收藏、匹配人物图片、真实播放续播字幕、外部兜底与两步删除", async ({
  page,
}, info) => {
  test.setTimeout(90000);
  await login(page);
  const form = page.getByRole("form", { name: "新建媒体库" });
  await form.getByLabel("媒体库名称", { exact: true }).fill("交互电影");
  await form.getByLabel("媒体目录路径").fill(media);
  await form.getByLabel("TMDB", { exact: true }).uncheck();
  await form.getByLabel("TVDB", { exact: true }).uncheck();
  await form.getByRole("button", { name: "创建并扫描" }).click();
  const row = page.getByRole("region", { name: "交互电影", exact: true });
  await expect(
    row.getByText("扫描完成 · 2 个文件", { exact: true }),
  ).toBeVisible();
  library = (
    (await (
      await page.request.get(origin + "/api/libraries")
    ).json()) as Schema["LibraryPage"]
  ).items[0];
  await row.getByRole("button", { name: "重新扫描" }).click();
  await expect(row.getByRole("button", { name: "重新扫描" })).toBeEnabled();
  await row.getByRole("link", { name: "交互电影 →" }).click();
  await expect(page.getByTestId("media-card")).toHaveCount(2);
  await page.getByLabel("搜索", { exact: true }).fill("中文");
  await page.getByRole("button", { name: "搜索", exact: true }).click();
  await expect(page.getByTestId("media-card")).toHaveCount(1);
  await page.getByTestId("media-card").getByRole("button").click();
  await expect(
    page.getByTestId("media-card").getByRole("button"),
  ).toHaveAttribute("aria-pressed", "true");
  await page.getByTestId("media-card").getByRole("link").click();
  id = new URL(page.url()).pathname.split("/").pop()!;
  await page.goto(origin + "/settings/metadata");
  const settings = page.getByRole("form", { name: "MetaTube 设置" });
  await settings.getByLabel("服务地址").fill("http://127.0.0.1:18095");
  await settings
    .getByLabel("Token", { exact: true })
    .fill("isolated-browser-metatube-token");
  await settings
    .getByRole("button", { name: "保存 MetaTube", exact: true })
    .click();
  await expect(settings.getByText(/设置已保存/)).toBeVisible();
  await page.goto(`${origin}/media/${id}`);
  await page.getByRole("button", { name: "匹配影片资料", exact: true }).click();
  const match = page.getByRole("dialog", { name: "匹配影片资料" });
  await match.getByLabel("资料来源").selectOption("metatube");
  await match.getByLabel("片名或编号").fill("ABC-123");
  await match.getByRole("button", { name: "搜索候选" }).click();
  await match
    .getByRole("button", { name: "选择 MetaTube 示例影片", exact: true })
    .click();
  await expect(match).toHaveCount(0);
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(
    "MetaTube 示例影片",
  );
  await expect
    .poll(() =>
      page
        .locator("main img")
        .evaluateAll(
          (images) =>
            images.filter((img) => (img as HTMLImageElement).naturalWidth > 0)
              .length,
        ),
    )
    .toBeGreaterThan(0);
  await page
    .getByRole("link", { name: "MetaTube 演员 · 演员", exact: true })
    .click();
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(
    "MetaTube 演员",
  );
  await expect(page.getByTestId("media-card")).toHaveCount(1);
  await page.getByTestId("media-card").getByRole("link").click();
  await page.screenshot({ path: info.outputPath("details-desktop.png") });
  // Real HTMLVideoElement decoding. No player method or API is mocked in this chain.
  await page.getByRole("button", { name: "网页播放", exact: true }).click();
  const player = page.getByRole("dialog", { name: "播放 MetaTube 示例影片" });
  const video = player.locator("video");
  await expect(player.locator("[data-player-state]")).toHaveAttribute(
    "data-player-state",
    "playing",
  );
  await expect
    .poll(() =>
      video.evaluate(
        (v) =>
          (v as HTMLVideoElement).getVideoPlaybackQuality().totalVideoFrames,
      ),
    )
    .toBeGreaterThan(0);
  await video.evaluate((v) => {
    (v as HTMLVideoElement).currentTime = 12;
  });
  await expect
    .poll(() => video.evaluate((v) => (v as HTMLVideoElement).currentTime))
    .toBeGreaterThanOrEqual(12);
  const subtitles = player.getByRole("combobox");
  await subtitles.selectOption({ label: "中文 & #.zh.srt" });
  await expect
    .poll(() =>
      video.evaluate(
        (v) => (v as HTMLVideoElement).textTracks[0]?.cues?.length || 0,
      ),
    )
    .toBe(1);
  await expect
    .poll(() =>
      video.evaluate((v) => (v as HTMLVideoElement).textTracks[0]?.mode),
    )
    .toBe("showing");
  await subtitles.selectOption({ label: "中文 & #.en.vtt" });
  await expect(video.locator("track")).toHaveCount(1);
  await subtitles.selectOption("");
  await expect(video.locator("track")).toHaveCount(0);
  await video.evaluate((v) => (v as HTMLVideoElement).pause());
  await player.getByRole("button", { name: "关闭播放器", exact: true }).click();
  await expect(player).toHaveCount(0);
  const progress: { progress: Schema["Progress"] } = await (
    await page.request.get(`${origin}/api/media/${id}/progress`)
  ).json();
  expect(progress.progress.position).toBeGreaterThanOrEqual(12);
  expect(progress.progress.completed).toBe(false);
  await page.getByRole("button", { name: "继续播放", exact: true }).click();
  await expect(player.locator("[data-player-state]")).toHaveAttribute(
    "data-player-state",
    "playing",
  );
  await expect
    .poll(() => video.evaluate((v) => (v as HTMLVideoElement).currentTime))
    .toBeGreaterThanOrEqual(12);
  await player.getByRole("button", { name: "关闭播放器", exact: true }).click();
  await expect(player).toHaveCount(0);
  await page.getByRole("button", { name: "从头播放", exact: true }).click();
  await expect(player.locator("[data-player-state]")).toHaveAttribute(
    "data-player-state",
    "playing",
  );
  expect(
    await video.evaluate((v) => (v as HTMLVideoElement).currentTime),
  ).toBeLessThan(5);
  await video.evaluate((v) => {
    (v as HTMLVideoElement).currentTime =
      (v as HTMLVideoElement).duration - 0.2;
  });
  await expect
    .poll(
      async () =>
        (
          (await (
            await page.request.get(`${origin}/api/media/${id}/progress`)
          ).json()) as { progress: Schema["Progress"] }
        ).progress.completed,
    )
    .toBe(true);
  await player.getByRole("button", { name: "关闭播放器", exact: true }).click();
  await expect(player).toHaveCount(0);
  const address = await page.getByLabel("串流地址（可手动复制）").inputValue();
  expect(new URL(address).pathname).toBe(`/stream/${id}`);
  await expect(
    page.getByRole("link", { name: "用 IINA 打开" }),
  ).toHaveAttribute("href", `iina://open?url=${encodeURIComponent(address)}`);
  await expect(
    page.getByRole("link", { name: "用 PotPlayer 打开" }),
  ).toHaveAttribute("href", `potplayer://${address}`);
  await expect(page.getByRole("link", { name: "用 VLC 打开" })).toHaveAttribute(
    "href",
    `vlc://${address}`,
  );
  await page.evaluate(() =>
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText: () => Promise.reject(new Error("denied")) },
    }),
  );
  await page.getByRole("button", { name: "复制串流地址" }).click();
  await expect(
    page.getByText("剪贴板不可用，请手动复制完整地址。"),
  ).toBeVisible();
  await expect(page.getByLabel("串流地址（可手动复制）")).toBeFocused();
  await page.setViewportSize({ width: 390, height: 844 });
  await page.getByRole("button", { name: "删除源文件", exact: true }).click();
  const deletion = page.getByRole("dialog", { name: "删除源文件" });
  await expect(
    deletion.getByText(join(realpathSync(media), "中文 & #.webm"), {
      exact: true,
    }),
  ).toBeVisible();
  await expect(
    deletion.getByRole("button", { name: "永久删除此文件" }),
  ).toBeDisabled();
  await page.screenshot({ path: info.outputPath("deletion-390.png") });
  await expect
    .poll(() =>
      page.evaluate(() => document.documentElement.scrollWidth <= innerWidth),
    )
    .toBe(true);
  await page.keyboard.press("Escape");
  await expect(deletion).toHaveCount(0);
  expect(existsSync(join(media, "中文 & #.webm"))).toBe(true);
  await expect(
    page.getByRole("button", { name: "删除源文件", exact: true }),
  ).toBeFocused();
  await page.getByRole("button", { name: "删除源文件", exact: true }).click();
  await deletion.getByRole("checkbox").check();
  await deletion.getByRole("button", { name: "永久删除此文件" }).click();
  await expect(deletion.getByRole("status")).toContainText("索引已更新");
  await deletion.getByRole("button", { name: "关闭结果" }).click();
  expect(existsSync(join(media, "中文 & #.webm"))).toBe(false);
  for (const name of [
    "中文 & #.zh.srt",
    "中文 & #.en.vtt",
    "保留相邻视频.webm",
    "poster.txt",
  ])
    expect(existsSync(join(media, name))).toBe(true);
  await expect(
    page.getByRole("button", { name: "删除源文件", exact: true }),
  ).toBeDisabled();
});

test("真实媒体库与匹配 CAS 保留草稿/原选择，资料刷新与失败页使用新契约", async ({
  page,
}) => {
  await login(page);
  const row = page.getByRole("region", { name: "交互电影", exact: true });
  await row.getByRole("button", { name: "编辑媒体库" }).click();
  const form = row.getByRole("form");
  await form.getByLabel("媒体库名称", { exact: true }).fill("我的草稿");
  expect(
    (
      await page.request.patch(`${origin}/api/libraries/${library.id}`, {
        data: { revision: library.revision, name: "另一个会话" },
      })
    ).status(),
  ).toBe(200);
  await form.getByRole("button", { name: "保存媒体库" }).click();
  await expect(form.getByText(/草稿已保留/)).toBeVisible();
  await expect(form.getByLabel("媒体库名称", { exact: true })).toHaveValue(
    "我的草稿",
  );
  await form.getByRole("button", { name: "读取最新版本，保留草稿" }).click();
  const edited = page.getByRole("form", { name: "编辑 另一个会话" });
  await edited.getByRole("button", { name: "保存媒体库" }).click();
  await expect(
    page.getByRole("region", { name: "我的草稿", exact: true }),
  ).toBeVisible();
  const catalog: Schema["CatalogPage"] = await (
    await page.request.get(`${origin}/api/catalog?libraryId=${library.id}`)
  ).json();
  const spare = catalog.items.find((c) => c.title === "保留相邻视频")!;
  await page.goto(`${origin}/media/${spare.id}`);
  await page.getByRole("button", { name: "匹配影片资料", exact: true }).click();
  const modal = page.getByRole("dialog", { name: "匹配影片资料" });
  await modal.getByLabel("资料来源").selectOption("metatube");
  await modal.getByRole("button", { name: "搜索候选" }).click();
  await expect(
    modal.getByRole("button", { name: "选择 MetaTube 示例影片" }),
  ).toBeVisible();
  const detail: Schema["MediaDetail"] = await (
    await page.request.get(`${origin}/api/media/${spare.id}`)
  ).json();
  const candidate: Schema["CandidatePage"] = await (
    await page.request.get(
      `${origin}/api/metadata/subjects/${detail.match.subjectId}/candidates?source=metatube&q=ABC`,
    )
  ).json();
  expect(
    (
      await page.request.put(
        `${origin}/api/metadata/subjects/${detail.match.subjectId}/selection`,
        {
          data: {
            subjectRevision: candidate.subjectRevision,
            expectedRevision: candidate.bindingRevision,
            ref: candidate.items[0].ref,
          },
        },
      )
    ).status(),
  ).toBe(200);
  await modal.getByRole("button", { name: "选择 MetaTube 示例影片" }).click();
  await expect(modal.getByText(/候选或选择版本已变化/)).toBeVisible();
  await expect(
    modal.getByRole("button", { name: "选择 MetaTube 示例影片" }),
  ).toBeDisabled();
  await page.keyboard.press("Escape");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(
    "MetaTube 示例影片",
  );
  await page.goto(origin + "/libraries");
  await page
    .getByRole("region", { name: "我的草稿", exact: true })
    .getByRole("button", { name: "刷新影片资料" })
    .click();
  await expect(
    page.getByText("资料刷新已排队；不会改动源文件。"),
  ).toBeVisible();
  await page.goto(origin + "/tasks");
  await expect(page.getByRole("heading", { name: "失败目标" })).toBeVisible();
  await page.getByRole("button", { name: "更新失败列表" }).click();
});

test("自动播放受阻可恢复；慢进度只保留最新快照且三秒关闭允许退出", async ({
  page,
}) => {
  test.setTimeout(45000);
  await login(page);
  const catalog: Schema["CatalogPage"] = await (
    await page.request.get(`${origin}/api/catalog?libraryId=${library.id}`)
  ).json();
  const spare = catalog.items.find(
    (c) => c.entryType === "media" && c.state === "available",
  )!;
  await page.goto(`${origin}/media/${spare.id}`);
  // Only force the browser's autoplay policy rejection. The retry uses native play.
  await page.evaluate(() => {
    const play = HTMLMediaElement.prototype.play;
    let first = true;
    HTMLMediaElement.prototype.play = function () {
      if (first) {
        first = false;
        return Promise.reject(
          new DOMException("user gesture", "NotAllowedError"),
        );
      }
      return play.call(this);
    };
  });
  await page
    .getByRole("button", { name: /网页播放|继续播放/, exact: true })
    .click();
  const dialog = page.getByRole("dialog", { name: /播放 MetaTube/ });
  await expect(dialog.locator("[data-player-state]")).toHaveAttribute(
    "data-player-state",
    "blocked",
  );
  await dialog.getByRole("button", { name: "播放视频", exact: true }).click();
  await expect(dialog.locator("[data-player-state]")).toHaveAttribute(
    "data-player-state",
    "playing",
  );
  let requests = 0;
  let release!: () => void;
  const held = new Promise<void>((done) => {
    release = done;
  });
  await page.route(`**/api/media/${spare.id}/progress`, async (route) => {
    if (route.request().method() !== "PUT") {
      await route.continue();
      return;
    }
    requests++;
    const response = await route.fetch();
    await held;
    await route.fulfill({ response }).catch(() => {});
  });
  const video = dialog.locator("video");
  await video.evaluate((v) => {
    (v as HTMLVideoElement).currentTime = 8;
  });
  await expect.poll(() => requests).toBe(1);
  for (const position of [10, 15, 20]) {
    await video.evaluate((v, p) => {
      (v as HTMLVideoElement).currentTime = p;
    }, position);
    await page.waitForTimeout(60);
  }
  await video.evaluate((v) => (v as HTMLVideoElement).pause());
  const began = Date.now();
  await dialog.getByRole("button", { name: "关闭播放器", exact: true }).click();
  await expect(dialog.getByText("正在保存进度，最多等待 3 秒…")).toBeVisible();
  await expect(dialog).toHaveCount(0, { timeout: 4500 });
  expect(Date.now() - began).toBeLessThan(4000);
  expect(requests).toBe(1);
  await expect(
    page.getByText(/已关闭播放器；本次进度保存未确认/),
  ).toBeVisible();
  release();
  await page.unrouteAll({ behavior: "wait" });
});

test("实际删除后响应丢失显示结果未知且不重试", async ({ page }) => {
  await login(page);
  const catalog: Schema["CatalogPage"] = await (
    await page.request.get(`${origin}/api/catalog?libraryId=${library.id}`)
  ).json();
  const spare = catalog.items.find(
    (c) => c.entryType === "media" && c.state === "available",
  )!;
  let deletes = 0;
  await page.route(/\/api\/media\/[^/]+\/delete$/, async (route) => {
    deletes++;
    const response = await route.fetch(); // real unlink/index commit, then lose its response
    expect(response.status()).toBe(200);
    await route.abort("failed");
  });
  await page.goto(`${origin}/media/${spare.id}`);
  await page.getByRole("button", { name: "删除源文件", exact: true }).click();
  const deletion = page.getByRole("dialog", { name: "删除源文件" });
  await deletion.getByRole("checkbox").check();
  await deletion.getByRole("button", { name: "永久删除此文件" }).click();
  await expect.poll(() => deletes).toBe(1);
  await expect(deletion.getByRole("status")).toContainText("删除结果未知");
  await page.waitForTimeout(1600);
  expect(deletes).toBe(1);
  await expect(
    deletion.getByRole("button", { name: "永久删除此文件" }),
  ).toHaveCount(0);
  expect(existsSync(join(media, "保留相邻视频.webm"))).toBe(false);
});
