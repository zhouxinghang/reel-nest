import type { ReactNode } from "react";
import { QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { API } from "../../shared/api";
import { APIProvider, createQueryClient } from "../../shared/services";
import { status } from "../../shared/test-fixtures";
import { keys } from "../catalog/queries";
import { taskInterval, useTaskStatus } from "./TaskStatus";

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});
describe("single foreground task subscription", () => {
  it("uses 1.5s active / 30s idle and failure backoff", () => {
    expect(taskInterval()).toBe(30000);
    expect(taskInterval(status)).toBe(30000);
    expect(taskInterval({ ...status, queued: 1 })).toBe(1500);
    expect(taskInterval({ ...status, running: 1 }, true)).toBe(30000);
  });
  it("does not invalidate on first load; only a changed revision refreshes active projections", async () => {
    vi.useFakeTimers();
    let data = status;
    const api = vi.fn().mockImplementation(async () => data) as unknown as API;
    const client = createQueryClient();
    const invalidation = vi.spyOn(client, "invalidateQueries");
    const wrapper = ({ children }: { children: ReactNode }) => (
      <APIProvider value={api}>
        <QueryClientProvider client={client}>{children}</QueryClientProvider>
      </APIProvider>
    );
    const hook = renderHook(useTaskStatus, { wrapper });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5);
    });
    expect(hook.result.current.data?.revision).toBe(1);
    expect(invalidation).not.toHaveBeenCalled();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(29900);
    });
    expect(api).toHaveBeenCalledTimes(1);
    data = { ...status, revision: 2, running: 1 };
    await act(async () => {
      await vi.advanceTimersByTimeAsync(110);
    });
    expect(api).toHaveBeenCalledTimes(2);
    expect(invalidation).toHaveBeenCalledTimes(1);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1510);
    });
    expect(api).toHaveBeenCalledTimes(3);
    expect(invalidation).toHaveBeenCalledTimes(1);
    hook.unmount();
    client.clear();
  });
  it("pauses while hidden and aborts an in-flight status read", async () => {
    vi.useFakeTimers();
    let hidden = false;
    vi.spyOn(document, "visibilityState", "get").mockImplementation(() =>
      hidden ? "hidden" : "visible",
    );
    const client = createQueryClient();
    let signal: AbortSignal | undefined;
    const api = vi.fn().mockImplementation((_method, _path, options) => {
      signal = options.signal;
      return new Promise(() => {});
    }) as unknown as API;
    const wrapper = ({ children }: { children: ReactNode }) => (
      <APIProvider value={api}>
        <QueryClientProvider client={client}>{children}</QueryClientProvider>
      </APIProvider>
    );
    const hook = renderHook(useTaskStatus, { wrapper });
    expect(api).toHaveBeenCalledTimes(1);
    hidden = true;
    await act(async () => {
      document.dispatchEvent(new Event("visibilitychange"));
    });
    expect(signal?.aborted).toBe(true);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(90000);
    });
    expect(api).toHaveBeenCalledTimes(1);
    hidden = false;
    await act(async () => {
      document.dispatchEvent(new Event("visibilitychange"));
    });
    expect(api).toHaveBeenCalledTimes(2);
    hook.unmount();
    client.removeQueries({ queryKey: keys.tasks });
  });
});
