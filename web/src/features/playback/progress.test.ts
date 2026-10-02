import { describe, expect, it, vi } from "vitest";
import { deferred } from "../../shared/test-fixtures";
import {
  ProgressWriter,
  progressSnapshot,
  playerTransition,
  mediaError,
} from "./progress";
import { playerLinks } from "./External";
import { deletionMessage } from "./Deletion";
const value = (position: number) => ({
  position,
  duration: 100,
  completed: false,
});
describe("bounded playback control", () => {
  it("keeps exactly one inflight and one latest snapshot during a thousand seeks", async () => {
    const first = deferred<void>();
    const second = deferred<void>();
    const write = vi
      .fn()
      .mockImplementationOnce(() => first.promise)
      .mockImplementationOnce(() => second.promise);
    const writer = new ProgressWriter(write);
    writer.submit(value(1));
    await Promise.resolve();
    for (let i = 2; i <= 1000; i++) writer.submit(value(i / 10));
    expect(write).toHaveBeenCalledTimes(1);
    first.resolve();
    await Promise.resolve();
    await Promise.resolve();
    expect(write).toHaveBeenCalledTimes(2);
    expect(write.mock.calls[1][0].position).toBe(100);
    second.resolve();
    expect(await writer.finish()).toBe(true);
  });
  it("failed writes never replay; a latest pending snapshot may still save", async () => {
    const first = deferred<void>();
    const write = vi
      .fn()
      .mockImplementationOnce(() => first.promise)
      .mockResolvedValue(undefined);
    const writer = new ProgressWriter(write);
    writer.submit(value(1));
    await Promise.resolve();
    writer.submit(value(2));
    first.reject(new Error("offline"));
    expect(await writer.finish()).toBe(true);
    expect(write).toHaveBeenCalledTimes(2);
    expect(write.mock.calls[1][0].position).toBe(2);
  });
  it("close times out at three seconds, aborts inflight and discards pending rather than replaying later", async () => {
    vi.useFakeTimers();
    try {
      const pending = deferred<void>();
      const write = vi.fn(() => pending.promise);
      const report = vi.fn();
      const writer = new ProgressWriter(write, report);
      writer.submit(value(1));
      await Promise.resolve();
      writer.submit(value(2));
      const done = writer.finish();
      await vi.advanceTimersByTimeAsync(2999);
      expect(report).not.toHaveBeenCalled();
      await vi.advanceTimersByTimeAsync(1);
      expect(await done).toBe(false);
      expect(
        (write.mock.calls[0] as unknown as [unknown, AbortSignal])[1].aborted,
      ).toBe(true);
      pending.resolve();
      await Promise.resolve();
      writer.submit(value(3));
      expect(write).toHaveBeenCalledTimes(1);
      expect(report).not.toHaveBeenCalled();
    } finally {
      vi.useRealTimers();
    }
  });
  it("reports a failed final write honestly and handles synchronous transport throws", async () => {
    const write = vi.fn(() => {
      throw new Error("failed");
    });
    const writer = new ProgressWriter(write);
    writer.submit(value(1));
    expect(await writer.finish()).toBe(false);
    expect(write).toHaveBeenCalledTimes(1);
  });
  it("only an ended event can complete finite in-range progress", () => {
    expect(
      progressSnapshot({ currentTime: 99.9, duration: 100 }, false)?.completed,
    ).toBe(false);
    expect(
      progressSnapshot({ currentTime: 100, duration: 100 }, true)?.completed,
    ).toBe(true);
    expect(
      progressSnapshot({ currentTime: 0, duration: NaN }, false),
    ).toBeNull();
    expect(
      progressSnapshot({ currentTime: Infinity, duration: 100 }, false),
    ).toBeNull();
    expect(
      progressSnapshot({ currentTime: 4, duration: 100 }, true)?.completed,
    ).toBe(false);
  });
  it("closing is terminal and playback errors distinguish read failure from codecs", () => {
    for (const next of ["playing", "paused", "failed", "blocked"] as const)
      expect(playerTransition("closing", next)).toBe("closing");
    expect(mediaError(2)).toContain("读取中断");
    expect(mediaError(3)).toContain("解码");
    expect(mediaError(4)).toContain("不转码");
  });
  it("protocol links preserve UTF-8, ampersands and encoded tokens", () => {
    const url = new URL(
      "/stream/%E4%B8%AD%20%26?token=a%2Bb%3D",
      "http://nas:8090",
    ).href;
    const links = playerLinks(url);
    expect(new URL(links.IINA).searchParams.get("url")).toBe(url);
    expect(links.PotPlayer).toBe(`potplayer://${url}`);
    expect(links.VLC).toBe(`vlc://${url}`);
  });
  it("HTTP 200 partial/unknown deletion never becomes full success", () => {
    const base = { message: "", retryable: false as const, requestId: "r" };
    expect(
      deletionMessage({
        ...base,
        fileOutcome: "deleted",
        indexOutcome: "pending",
      }),
    ).toContain("索引更新未完成");
    expect(
      deletionMessage({
        ...base,
        fileOutcome: "unknown",
        indexOutcome: "pending",
      }),
    ).toContain("不要重复删除");
    expect(
      deletionMessage({
        ...base,
        fileOutcome: "not_deleted",
        indexOutcome: "updated",
      }),
    ).toContain("未删除");
  });
});
