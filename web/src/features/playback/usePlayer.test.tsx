import type { ReactNode } from "react";
import { QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import type { API } from "../../shared/api";
import { APIProvider, createQueryClient } from "../../shared/services";
import { usePlayer } from "./usePlayer";
it("restores before play, saves every ten seconds/pause/seek/hidden and never completes merely near the end", async () => {
  vi.useFakeTimers();
  const writes: { position: number; completed: boolean }[] = [];
  const api = vi.fn(async (method, _path, options) => {
    if (method === "get")
      return {
        mediaId: "file",
        contentPath: "/api/media/file/content",
        progress: {
          position: 12,
          duration: 100,
          completed: false,
          updatedAt: 1,
        },
      };
    writes.push(options.body);
    return { ...options.body, updatedAt: 2 };
  }) as unknown as API;
  const client = createQueryClient();
  const wrapper = ({ children }: { children: ReactNode }) => (
    <APIProvider value={api}>
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    </APIProvider>
  );
  const close = vi.fn();
  const hook = renderHook(() => usePlayer("file", "lib", false, close), {
    wrapper,
  });
  const element = document.createElement("video");
  let paused = false;
  Object.defineProperties(element, {
    duration: { value: 100 },
    paused: { get: () => paused },
  });
  const positions: number[] = [];
  element.play = vi.fn(async () => {
    positions.push(element.currentTime);
  });
  element.pause = vi.fn(() => {
    paused = true;
  });
  hook.result.current.video.current = element;
  try {
    await act(async () => {});
    await act(async () => hook.result.current.events.onLoadedMetadata());
    expect(element.currentTime).toBe(12);
    expect(element.play).not.toHaveBeenCalled();
    await act(async () => hook.result.current.events.onSeeked());
    expect(positions).toEqual([12]);
    expect(writes).toHaveLength(0);
    await act(async () => hook.result.current.events.onPlay());
    element.currentTime = 22;
    await act(async () => {
      await vi.advanceTimersByTimeAsync(9999);
    });
    expect(writes).toHaveLength(0);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });
    expect(writes.at(-1)?.position).toBe(22);
    element.currentTime = 40;
    await act(async () => hook.result.current.events.onSeeked());
    expect(writes.at(-1)?.position).toBe(40);
    element.currentTime = 99.9;
    paused = true;
    await act(async () => hook.result.current.events.onPause());
    expect(writes.at(-1)).toMatchObject({ position: 99.9, completed: false });
    vi.spyOn(document, "visibilityState", "get").mockReturnValue("hidden");
    element.currentTime = 55;
    await act(async () =>
      document.dispatchEvent(new Event("visibilitychange")),
    );
    expect(writes.at(-1)?.position).toBe(55);
    element.currentTime = 100;
    await act(async () => hook.result.current.events.onEnded());
    expect(writes.at(-1)?.completed).toBe(true);
    await act(async () => hook.result.current.close());
    expect(close).toHaveBeenCalledWith("已关闭播放器，进度保存完成。");
    await act(async () => hook.result.current.events.onPlay());
    expect(hook.result.current.state).toBe("closing");
  } finally {
    hook.unmount();
    client.clear();
    vi.useRealTimers();
    vi.restoreAllMocks();
  }
});
