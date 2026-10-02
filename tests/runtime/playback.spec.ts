import { expect, test } from "@playwright/test";
import { spawn, type ChildProcess } from "node:child_process";
import { copyFileSync, mkdirSync, mkdtempSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import type { components } from "../../web/src/generated/api";

type Schemas = components["schemas"];
const origin = "http://127.0.0.1:18096";
let child: ChildProcess;
let media: string;

test.beforeAll(async () => {
  const root = process.env.REELNEST_TEST_ROOT!;
  const run = mkdtempSync(join(root, "runtime-"));
  media = join(run, "media");
  mkdirSync(media, { recursive: true });
  copyFileSync("tests/fixtures/playback.webm", join(media, "中文 & #.webm"));
  writeFileSync(
    join(media, "中文 & #.zh.srt"),
    "1\n00:00:00,000 --> 00:00:59,900\n原生字幕验证\n",
  );
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
        return (await fetch(`${origin}/healthz`)).status;
      } catch {
        return 0;
      }
    })
    .toBe(200);
});
test.afterAll(async () => {
  if (!child || child.exitCode !== null) return;
  await new Promise<void>((resolve) => {
    child.once("exit", () => resolve());
    child.kill("SIGTERM");
  });
});

test("新 runtime 原生 VP8、拖动 Range、字幕与无会话串流（非新 UI 验收）", async ({
  browser,
}) => {
  test.setTimeout(60000);
  const context = await browser.newContext({ baseURL: origin });
  try {
    expect(
      (
        await context.request.post("/api/login", {
          data: { username: "family", password: "browser-test-password" },
        })
      ).status(),
    ).toBe(200);
    const added = await context.request.post("/api/libraries", {
      data: { name: "原生视频测试", path: media, kind: "movie", sources: [] },
    });
    expect(added.status()).toBe(201);
    const library: Schemas["Library"] = await added.json();
    await expect
      .poll(async () => {
        const scan: Schemas["Scan"] = await (
          await context.request.get(`/api/scans/${library.latestScanId}`)
        ).json();
        return scan.state;
      })
      .toBe("completed");
    const catalog: Schemas["CatalogPage"] = await (
      await context.request.get(`/api/catalog?libraryId=${library.id}`)
    ).json();
    const file = catalog.items[0];
    if (file.entryType !== "media") throw new Error("expected media card");
    const base = `/api/media/${file.id}`;
    const preflight: Schemas["Playback"] = await (
      await context.request.get(`${base}/playback`)
    ).json();
    expect(preflight.progress).toBeNull();
    const subs: Schemas["Subtitles"] = await (
      await context.request.get(`${base}/subtitles`)
    ).json();
    expect(subs.items).toHaveLength(1);
    const page = await context.newPage();
    const network = await context.newCDPSession(page);
    await network.send("Network.enable");
    await network.send("Network.emulateNetworkConditions", {
      offline: false,
      latency: 0,
      downloadThroughput: 256 * 1024,
      uploadThroughput: -1,
    });
    const ranges: string[] = [];
    page.on("request", (request) => {
      if (request.url().endsWith(preflight.contentPath))
        ranges.push(request.headers().range || "");
    });
    await page.goto("/__native-player");
    await page.locator("video").evaluate(
      (video: HTMLVideoElement, data) => {
        video.src = data.content;
        const track = document.createElement("track");
        track.kind = "subtitles";
        track.srclang = "zh";
        track.default = true;
        track.src = data.subtitle;
        video.append(track);
      },
      {
        content: preflight.contentPath,
        subtitle: `${base}/subtitles/${subs.items[0].id}`,
      },
    );
    await page.locator("video").evaluate((v: HTMLVideoElement) => v.play());
    await expect
      .poll(() =>
        page.locator("video").evaluate((v: HTMLVideoElement) => ({
          decoded: v.getVideoPlaybackQuality().totalVideoFrames > 0,
          moved: v.currentTime > 0,
          width: v.videoWidth > 0,
          caption: (v.textTracks[0]?.activeCues?.[0] as VTTCue)?.text,
        })),
      )
      .toMatchObject({
        decoded: true,
        moved: true,
        width: true,
        caption: "原生字幕验证",
      });
    await expect
      .poll(() =>
        page
          .locator("video")
          .evaluate(
            (v: HTMLVideoElement) => v.textTracks[0]?.activeCues?.length,
          ),
      )
      .toBe(1);
    await page.locator("video").evaluate((v: HTMLVideoElement) => {
      v.currentTime = 45;
    });
    await expect
      .poll(() =>
        page
          .locator("video")
          .evaluate(
            (v: HTMLVideoElement) =>
              !v.seeking && v.currentTime >= 45 && v.readyState >= 2,
          ),
      )
      .toBe(true);
    expect(ranges.some((range) => /^bytes=[1-9]\d*-/.test(range))).toBe(true);
    await page.locator("video").evaluate((v: HTMLVideoElement) => v.pause());
    // Native playback/streaming must not silently invent household progress.
    expect(
      await (await context.request.get(`${base}/progress`)).json(),
    ).toEqual({
      progress: null,
    });
    const link: Schemas["StreamLink"] = await (
      await context.request.post(`${base}/stream-link`, { data: {} })
    ).json();
    expect(
      (await context.request.post("/api/logout", { data: {} })).status(),
    ).toBe(200);
    expect((await context.request.get(preflight.contentPath)).status()).toBe(
      401,
    );
    await network.send("Network.emulateNetworkConditions", {
      offline: false,
      latency: 0,
      downloadThroughput: -1,
      uploadThroughput: -1,
    });
    await page.locator("video").evaluate(async (v: HTMLVideoElement, path) => {
      v.querySelector("track")?.remove();
      v.src = path;
      await v.play();
    }, link.path);
    await expect
      .poll(() =>
        page
          .locator("video")
          .evaluate(
            (v: HTMLVideoElement) =>
              v.currentTime > 0 &&
              v.getVideoPlaybackQuality().totalVideoFrames > 0,
          ),
      )
      .toBe(true);
  } finally {
    await context.close();
  }
});
