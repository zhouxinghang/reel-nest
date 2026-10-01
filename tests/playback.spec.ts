import { expect, test, type Page } from "@playwright/test";
import { copyFileSync, mkdirSync, writeFileSync, unlinkSync } from "node:fs";
import { join } from "node:path";
import { randomUUID } from "node:crypto";

async function login(page: Page) {
  await page.goto("/login");
  await page.getByLabel("家庭共享账号", { exact: true }).fill("family");
  await page.getByLabel("密码", { exact: true }).fill("browser-test-password");
  await page.getByRole("button", { name: "进入媒体库" }).click();
  await expect(
    page.getByRole("heading", { name: "媒体库", exact: true }),
  ).toBeVisible();
}

async function seek(page: Page, seconds: number) {
  const slider = page.getByLabel("播放位置", { exact: true });
  await slider.scrollIntoViewIfNeeded();
  const box = (await slider.boundingBox())!;
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  await page.mouse.down();
  await page.mouse.move(
    box.x + 8 + ((box.width - 16) * seconds) / 60,
    box.y + box.height / 2,
    { steps: 5 },
  );
  await page.mouse.up();
  await expect
    .poll(() =>
      page.locator("video").evaluate((v: HTMLVideoElement) => v.seeking),
    )
    .toBe(false);
}

test("真实 VP8 播放、定位、跨浏览器续播、看完、解码与文件错误", async ({
  page,
  context,
  browser,
}) => {
  test.setTimeout(90000);
  const root = join(
    process.env.REELNEST_TEST_ROOT!,
    "media",
    `playback-${randomUUID()}`,
  );
  mkdirSync(root, { recursive: true });
  const name = "中文 空格 & ? #.webm";
  copyFileSync("tests/fixtures/playback.webm", join(root, name));
  writeFileSync(
    join(root, "unsupported.mp4"),
    "intentionally invalid container",
  );
  copyFileSync("tests/fixtures/playback.webm", join(root, "missing.webm"));
  await login(page);
  await page.getByText("服务器状态", { exact: true }).click();
  await expect(
    page.getByRole("region", { name: "TMDB 设置与状态" }),
  ).toContainText("未配置 TMDB 凭据");
  const added = await context.request.post("/api/directories", {
    data: { path: root, kind: "movie" },
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
  const items = (
    await (await context.request.get("/api/library")).json()
  ).items.filter(
    (item: { directoryId: string }) => item.directoryId === directory.id,
  );
  const file = items.find(
    (item: { filename: string }) => item.filename === name,
  );
  const base = `/api/media/${file.id}`;
  const readProgress = async () =>
    (await context.request.get(`${base}/progress`)).json();
  // A real bandwidth-limited connection keeps the complete file out of the
  // browser cache, so seeking must issue another HTTP byte-range read.
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
    if (request.url().endsWith(`${base}/content`) && request.headers().range)
      ranges.push(request.headers().range);
  });
  await page.goto(`/media/${file.id}`);
  await page.getByRole("button", { name: "网页播放", exact: true }).click();
  await expect(page.locator("video")).toBeVisible();
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
      page.locator("video").evaluate((v: HTMLVideoElement) => v.currentTime),
    )
    .toBeGreaterThan(0.3);
  await expect
    .poll(() =>
      page
        .locator("video")
        .evaluate(
          (v: HTMLVideoElement) => v.getVideoPlaybackQuality().totalVideoFrames,
        ),
    )
    .toBeGreaterThan(1);
  // Drag the application's real seek control; no media-element mocks.
  ranges.length = 0;
  const framesBefore = await page
    .locator("video")
    .evaluate(
      (v: HTMLVideoElement) => v.getVideoPlaybackQuality().totalVideoFrames,
    );
  await seek(page, 45);
  await expect
    .poll(() => ranges.some((range) => /^bytes=[1-9][0-9]*-/.test(range)))
    .toBe(true);
  await expect
    .poll(() =>
      page
        .locator("video")
        .evaluate(
          (v: HTMLVideoElement) => v.getVideoPlaybackQuality().totalVideoFrames,
        ),
    )
    .toBeGreaterThan(framesBefore);
  await network.detach();
  const paused = await page
    .locator("video")
    .evaluate((v: HTMLVideoElement) => v.paused);
  if (paused)
    await page.getByRole("button", { name: "开始播放", exact: true }).click();
  await expect
    .poll(() =>
      page.locator("video").evaluate((v: HTMLVideoElement) => v.currentTime),
    )
    .toBeGreaterThan(45.2);
  await page.getByRole("button", { name: "暂停", exact: true }).click();
  await expect
    .poll(async () => (await readProgress()).position)
    .toBeGreaterThan(45);
  const saved = (await readProgress()).position;
  await page.goto("/library");
  await expect(
    page.getByRole("region", { name: "继续观看" }).getByRole("link", {
      name: new RegExp(name.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")),
    }),
  ).toBeVisible();
  const second = await browser.newContext();
  try {
    const other = await second.newPage();
    await login(other);
    await other.goto(`/media/${file.id}?play=1`);
    await expect
      .poll(() =>
        other.locator("video").evaluate((v: HTMLVideoElement) => v.currentTime),
      )
      .toBeGreaterThanOrEqual(saved);
    await expect(
      other.getByRole("button", { name: "暂停", exact: true }),
    ).toBeVisible();
    await expect
      .poll(() =>
        other.locator("video").evaluate((v: HTMLVideoElement) => v.currentTime),
      )
      .toBeGreaterThan(saved + 0.2);
    await seek(other, 59.5);
    await expect
      .poll(() =>
        other.locator("video").evaluate((v: HTMLVideoElement) => v.ended),
      )
      .toBe(true);
    await expect.poll(async () => (await readProgress()).completed).toBe(true);
  } finally {
    await second.close();
  }
  await page.reload();
  await expect(
    page.getByRole("region", { name: "继续观看" }).getByText(name),
  ).toHaveCount(0);
  await page.goto(`/media/${file.id}?play=1`);
  await expect
    .poll(() =>
      page.locator("video").evaluate((v: HTMLVideoElement) => v.duration),
    )
    .toBe(60);
  expect(
    await page
      .locator("video")
      .evaluate((v: HTMLVideoElement) => v.currentTime),
  ).toBeLessThan(3);
  await expect(
    page.getByText("未发现同目录匹配的 UTF-8 SRT/VTT 外挂字幕。", {
      exact: true,
    }),
  ).toBeVisible();
  await page.setViewportSize({ width: 1440, height: 960 });
  await page.screenshot({
    path: "test-results/player-desktop.png",
    fullPage: true,
  });
  await page.setViewportSize({ width: 390, height: 844 });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
  await page.screenshot({
    path: "test-results/player-mobile.png",
    fullPage: true,
  });
  const bad = items.find(
    (item: { filename: string }) => item.filename === "unsupported.mp4",
  );
  await page.goto(`/media/${bad.id}?play=1`);
  await expect(page.getByRole("alert")).toContainText("容器或编码可能不兼容");
  const missing = items.find(
    (item: { filename: string }) => item.filename === "missing.webm",
  );
  unlinkSync(join(root, "missing.webm"));
  await page.goto(`/media/${missing.id}?play=1`);
  await expect(
    page.getByRole("dialog", { name: "网页播放器" }).getByRole("alert"),
  ).toContainText("媒体文件已缺失");
});

test("外部播放器协议编码、可复制兜底与无会话只读串流", async ({
  page,
  context,
  browser,
}) => {
  const root = join(
    process.env.REELNEST_TEST_ROOT!,
    "media",
    `external-${randomUUID()}`,
  );
  mkdirSync(root, { recursive: true });
  const name = "外部 空格 & # ? 参数.webm";
  copyFileSync("tests/fixtures/playback.webm", join(root, name));
  await login(page);
  const directory = await (
    await context.request.post("/api/directories", {
      data: { path: root, kind: "movie" },
    })
  ).json();
  await expect
    .poll(
      async () =>
        (
          await (await context.request.get("/api/directories")).json()
        ).directories.find((d: { id: string }) => d.id === directory.id)
          .scanStatus,
    )
    .toBe("completed");
  const file = (
    await (await context.request.get("/api/library")).json()
  ).items.find((f: { directoryId: string }) => f.directoryId === directory.id);
  const base = `/api/media/${file.id}`;
  const before = await (await context.request.get(`${base}/progress`)).json();
  await page.goto(`/media/${file.id}`);
  await page.getByRole("button", { name: "复制链接", exact: true }).click();
  const address = page.getByLabel("串流地址（可手动复制）");
  await expect(address).toHaveValue(
    /^http:\/\/127\.0\.0\.1:18090\/stream\/[a-f0-9]+\?token=[a-f0-9]{64}$/,
  );
  const url = await address.inputValue();
  await expect(
    page.getByRole("link", { name: "用 IINA 打开" }),
  ).toHaveAttribute("href", `iina://open?url=${encodeURIComponent(url)}`);
  await expect(
    page.getByRole("link", { name: "用 PotPlayer 打开" }),
  ).toHaveAttribute("href", `potplayer://${url}`);
  await expect(page.getByText(/链接不会随时间自动失效/)).toBeVisible();
  await expect(page.getByText(/当前地址仅供本机使用/)).toBeVisible();
  await page.getByRole("button", { name: "复制链接", exact: true }).click();
  await expect(page.getByRole("status")).toContainText(
    /已复制串流地址|手动复制/,
  );
  await address.focus();
  expect(
    await address.evaluate(
      (input: HTMLInputElement) => input.selectionEnd! - input.selectionStart!,
    ),
  ).toBe(url.length);
  const independent = await browser.newContext();
  try {
    const response = await independent.request.get(url, {
      headers: { Range: "bytes=0-15" },
    });
    expect(response.status()).toBe(206);
    expect((await response.body()).length).toBe(16);
    expect(
      (
        await independent.request.get(
          `${new URL(url).origin}/api/library?${new URL(url).searchParams}`,
        )
      ).status(),
    ).toBe(401);
  } finally {
    await independent.close();
  }
  expect(await (await context.request.get(`${base}/progress`)).json()).toEqual(
    before,
  );
});

test("真实字幕渲染、SRT/VTT 切换、时间轴定位、关闭与错误不阻断视频", async ({
  page,
  context,
}) => {
  const root = join(
    process.env.REELNEST_TEST_ROOT!,
    "media",
    `subtitles-${randomUUID()}`,
  );
  mkdirSync(root, { recursive: true });
  copyFileSync("tests/fixtures/playback.webm", join(root, "字幕测试.webm"));
  writeFileSync(
    join(root, "字幕测试.zh.srt"),
    "\ufeff1\r\n00:00:00,000 --> 00:00:20,000\r\n家人的第一句字幕\r\n\r\n2\r\n00:00:20,000 --> 00:01:00,000\r\n定位之后的字幕\r\n",
  );
  writeFileSync(
    join(root, "字幕测试.en.vtt"),
    "WEBVTT\n\n00:00.000 --> 00:20.000\nWelcome home\n\n00:20.000 --> 01:00.000\nAfter seeking\n",
  );
  writeFileSync(
    join(root, "字幕测试.broken.srt"),
    "1\n00:99:00,000 --> 00:00:01,000\ninvalid",
  );
  await login(page);
  const directory = await (
    await context.request.post("/api/directories", {
      data: { path: root, kind: "movie" },
    })
  ).json();
  await expect
    .poll(
      async () =>
        (
          await (await context.request.get("/api/directories")).json()
        ).directories.find((d: { id: string }) => d.id === directory.id)
          .scanStatus,
    )
    .toBe("completed");
  const file = (
    await (await context.request.get("/api/library")).json()
  ).items.find((f: { directoryId: string }) => f.directoryId === directory.id);
  await page.goto(`/media/${file.id}?play=1`);
  const select = page.getByLabel("外挂字幕", { exact: true });
  await expect(select).toBeEnabled();
  await select.selectOption({ label: "字幕测试.zh.srt · SRT" });
  await expect(
    page.getByRole("button", { name: "暂停", exact: true }),
  ).toBeVisible();
  const active = () =>
    page.locator("video").evaluate((video: HTMLVideoElement) =>
      Array.from(video.textTracks)
        .filter((track) => track.mode === "showing")
        .flatMap((track) =>
          Array.from(track.activeCues || []).map((cue) => (cue as VTTCue).text),
        ),
    );
  await expect.poll(active).toEqual(["家人的第一句字幕"]);
  await page
    .locator("video")
    .screenshot({ path: "test-results/subtitles-srt.png" });
  await select.selectOption({ label: "字幕测试.en.vtt · VTT" });
  await expect.poll(active).toEqual(["Welcome home"]);
  await seek(page, 30);
  await expect.poll(active).toEqual(["After seeking"]);
  await page
    .locator("video")
    .screenshot({ path: "test-results/subtitles-vtt-seek.png" });
  await select.selectOption("");
  await expect.poll(active).toEqual([]);
  await expect(page.getByText("字幕已关闭", { exact: true })).toBeVisible();
  await page
    .locator("video")
    .screenshot({ path: "test-results/subtitles-off.png" });
  await select.selectOption({ label: "字幕测试.broken.srt · SRT" });
  await expect(page.getByRole("alert")).toContainText("字幕不可用");
  await expect.poll(active).toEqual([]);
  const time = await page
    .locator("video")
    .evaluate((v: HTMLVideoElement) => v.currentTime);
  await expect
    .poll(() =>
      page.locator("video").evaluate((v: HTMLVideoElement) => v.currentTime),
    )
    .toBeGreaterThan(time + 0.2);
  await select.selectOption({ label: "字幕测试.zh.srt · SRT" });
  await expect.poll(active).toEqual(["定位之后的字幕"]);
  await page.setViewportSize({ width: 390, height: 844 });
  await page.screenshot({
    path: "test-results/subtitles-mobile.png",
    fullPage: true,
  });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
});

test("自动播放拦截可重试、退出保存进度、串流失败重试和剪贴板兜底", async ({
  page,
  context,
}) => {
  const root = join(
    process.env.REELNEST_TEST_ROOT!,
    "media",
    `playback-ui-${randomUUID()}`,
  );
  mkdirSync(root, { recursive: true });
  copyFileSync("tests/fixtures/playback.webm", join(root, "播放交互.webm"));
  await login(page);
  const directory = await (
    await context.request.post("/api/directories", {
      data: { path: root, kind: "movie" },
    })
  ).json();
  await expect
    .poll(
      async () =>
        (
          await (await context.request.get("/api/directories")).json()
        ).directories.find((d: { id: string }) => d.id === directory.id)
          ?.scanStatus,
    )
    .toBe("completed");
  const file = (
    await (await context.request.get("/api/library")).json()
  ).items.find((f: { directoryId: string }) => f.directoryId === directory.id);
  const base = `/api/media/${file.id}`;
  await context.request.post(`${base}/progress`, {
    data: { position: 18, duration: 60, completed: false },
  });
  await page.addInitScript(() => {
    const play = HTMLMediaElement.prototype.play;
    let denied = false;
    HTMLMediaElement.prototype.play = function () {
      if (!denied) {
        denied = true;
        return Promise.reject(
          new DOMException("Autoplay blocked for test", "NotAllowedError"),
        );
      }
      return play.call(this);
    };
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: {
        writeText: () =>
          Promise.reject(
            new DOMException("Clipboard denied for test", "NotAllowedError"),
          ),
      },
    });
  });
  let failed = false;
  await page.route(`**${base}/stream-link`, async (route) => {
    if (!failed) {
      failed = true;
      await route.fulfill({
        status: 503,
        contentType: "application/json",
        body: JSON.stringify({ error: "测试串流暂不可用" }),
      });
    } else await route.continue();
  });
  await page.goto(`/media/${file.id}`);
  await expect(page.getByRole("alert")).toContainText("测试串流暂不可用");
  await expect(
    page.getByRole("button", { name: "复制链接", exact: true }),
  ).toBeDisabled();
  await page.getByRole("button", { name: "重试串流地址", exact: true }).click();
  await expect(
    page.getByRole("link", { name: "用 VLC 打开", exact: true }),
  ).toHaveAttribute("href", /^vlc:\/\/http/);
  await page.getByRole("button", { name: "复制链接", exact: true }).click();
  await expect(page.getByRole("status")).toContainText("剪贴板不可用");
  const address = page.getByLabel("串流地址（可手动复制）");
  await expect(address).toBeFocused();
  const url = await address.inputValue();
  expect(
    await address.evaluate(
      (element: HTMLInputElement) =>
        element.selectionEnd! - element.selectionStart!,
    ),
  ).toBe(url.length);
  // Exercise all actual link controls without launching apps on the test host.
  await page.evaluate(() => {
    document
      .querySelectorAll<HTMLAnchorElement>(".external-apps a")
      .forEach((link) => {
        link.addEventListener("click", (event) => event.preventDefault());
      });
  });
  for (const name of ["PotPlayer", "VLC", "IINA"]) {
    await page
      .getByRole("link", { name: `用 ${name} 打开`, exact: true })
      .click();
    await expect(page.getByRole("status")).toContainText(
      `已请求用 ${name} 打开`,
    );
  }
  await page.getByRole("button", { name: "网页播放", exact: true }).click();
  const player = page.getByRole("dialog", { name: "网页播放器", exact: true });
  await expect(player).toBeVisible();
  await expect(
    player.getByRole("status").filter({ hasText: "浏览器阻止了自动播放" }),
  ).toBeVisible();
  expect(
    await page
      .locator("video")
      .evaluate((video: HTMLVideoElement) => video.paused),
  ).toBe(true);
  expect(
    (await (await context.request.get(`${base}/progress`)).json()).position,
  ).toBe(18);
  await page.getByRole("button", { name: "开始播放", exact: true }).click();
  await expect
    .poll(() =>
      page
        .locator("video")
        .evaluate((video: HTMLVideoElement) => video.currentTime),
    )
    .toBeGreaterThan(18.2);
  await page.getByRole("button", { name: "关闭播放器", exact: true }).click();
  await expect(player).not.toBeVisible();
  expect(
    (await (await context.request.get(`${base}/progress`)).json()).position,
  ).toBeGreaterThan(18);
  await expect(
    page.getByRole("button", { name: "网页播放", exact: true }),
  ).toBeFocused();

  // Closing during the playback check must not start video in the background.
  let release!: () => void;
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  await page.route(`**${base}/playback`, async (route) => {
    await gate;
    await route.continue();
  });
  const requested = page.waitForRequest(`**${base}/playback`);
  await page.getByRole("button", { name: "网页播放", exact: true }).click();
  await requested;
  await page.getByRole("button", { name: "关闭播放器", exact: true }).click();
  await expect(player).not.toBeVisible();
  const checked = page.waitForResponse(`**${base}/playback`);
  release();
  await checked;
  await expect(page.locator("video")).toHaveCount(0);
  await expect(page.locator("html")).not.toHaveClass(/web-player-open/);
});
