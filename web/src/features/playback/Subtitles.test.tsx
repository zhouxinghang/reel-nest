import { createRef, type ReactNode } from "react";
import { QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import type { API } from "../../shared/api";
import { APIProvider, createQueryClient } from "../../shared/services";
import { deferred, page } from "../../shared/test-fixtures";
import { Subtitles } from "./Subtitles";
it("aborts old subtitle fetches, replaces/off/unmount revokes Blob URLs, and replaces pages", async () => {
  const slow = deferred<string>();
  let oldSignal: AbortSignal | undefined;
  const api = vi.fn((_method, path, options) => {
    if (path.endsWith("/subtitles"))
      return Promise.resolve({
        items: options.query.cursor
          ? [{ id: "3", filename: "third.srt", format: "srt" }]
          : [
              { id: "1", filename: "first.srt", format: "srt" },
              { id: "2", filename: "second.srt", format: "srt" },
            ],
        page: options.query.cursor ? page : { ...page, nextCursor: "next" },
      });
    if (options.path.subtitle === "1") {
      oldSignal = options.signal;
      return slow.promise;
    }
    return Promise.resolve("WEBVTT\n\n");
  }) as unknown as API;
  const create = vi
    .fn()
    .mockReturnValueOnce("blob:one")
    .mockReturnValue("blob:two");
  const revoke = vi.fn();
  vi.stubGlobal(
    "URL",
    Object.assign(URL, { createObjectURL: create, revokeObjectURL: revoke }),
  );
  Object.defineProperty(HTMLTrackElement.prototype, "track", {
    configurable: true,
    get: () => ({ mode: "disabled" }),
  });
  const client = createQueryClient();
  const video = createRef<HTMLVideoElement>();
  video.current = document.createElement("video");
  const wrapper = ({ children }: { children: ReactNode }) => (
    <APIProvider value={api}>
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    </APIProvider>
  );
  const view = render(<Subtitles id="file" video={video} />, { wrapper });
  await screen.findByText("first.srt");
  const select = screen.getByRole("combobox");
  fireEvent.change(select, { target: { value: "1" } });
  await waitFor(() => expect(oldSignal).toBeDefined());
  fireEvent.change(select, { target: { value: "2" } });
  await waitFor(() => expect(create).toHaveBeenCalledTimes(1));
  expect(oldSignal?.aborted).toBe(true);
  slow.resolve("WEBVTT stale");
  await Promise.resolve();
  expect(video.current.querySelectorAll("track")).toHaveLength(1);
  fireEvent.change(select, { target: { value: "" } });
  await waitFor(() => expect(revoke).toHaveBeenCalledWith("blob:one"));
  expect(video.current.querySelectorAll("track")).toHaveLength(0);
  fireEvent.click(screen.getByRole("button", { name: "下一页字幕" }));
  await screen.findByText("third.srt");
  expect(screen.queryByText("first.srt")).toBeNull();
  fireEvent.change(select, { target: { value: "3" } });
  await waitFor(() => expect(create).toHaveBeenCalledTimes(2));
  view.unmount();
  expect(revoke).toHaveBeenCalledWith("blob:two");
  client.clear();
  vi.unstubAllGlobals();
});
