import type { ReactNode } from "react";
import { QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import type { API } from "../../shared/api";
import { APIProvider, createQueryClient } from "../../shared/services";
import { ScanProvider, useScans } from "./Scans";
it("shell scan subscription is bounded, active-only, hidden-paused, and first status never invalidates", async () => {
  vi.useFakeTimers();
  let hidden = false;
  let complete = false;
  vi.spyOn(document, "visibilityState", "get").mockImplementation(() =>
    hidden ? "hidden" : "visible",
  );
  const api = vi.fn(async (_method, _path, options) => ({
    id: options.path.id,
    state: complete ? "completed" : "running",
    discoveredCount: 2,
  })) as unknown as API;
  const client = createQueryClient();
  const invalidate = vi.spyOn(client, "invalidateQueries");
  const wrapper = ({ children }: { children: ReactNode }) => (
    <APIProvider value={api}>
      <QueryClientProvider client={client}>
        <ScanProvider>{children}</ScanProvider>
      </QueryClientProvider>
    </APIProvider>
  );
  const hook = renderHook(useScans, { wrapper });
  try {
    await act(async () =>
      hook.result.current.watch(
        Array.from({ length: 60 }, (_, i) => String(i)),
      ),
    );
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5);
    });
    expect(api).toHaveBeenCalledTimes(50);
    expect(invalidate).not.toHaveBeenCalled();
    hidden = true;
    await act(async () =>
      document.dispatchEvent(new Event("visibilitychange")),
    );
    await act(async () => {
      await vi.advanceTimersByTimeAsync(90000);
    });
    expect(api).toHaveBeenCalledTimes(50);
    complete = true;
    hidden = false;
    await act(async () =>
      document.dispatchEvent(new Event("visibilitychange")),
    );
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5);
    });
    expect(api).toHaveBeenCalledTimes(100);
    expect(invalidate).toHaveBeenCalledTimes(2);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(60000);
    });
    expect(api).toHaveBeenCalledTimes(100);
  } finally {
    hook.unmount();
    client.clear();
    vi.useRealTimers();
    vi.restoreAllMocks();
  }
});
