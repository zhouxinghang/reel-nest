import { QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { cleanup } from "@testing-library/react";
import type { API } from "../../shared/api";
import { APIProvider, createQueryClient } from "../../shared/services";
import { External } from "./External";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

it("hides streaming explanations and the address until a manual-copy fallback is needed", async () => {
  const path = "/stream/file?token=synthetic";
  const url = new URL(path, window.location.origin).href;
  const writeText = vi.fn().mockResolvedValue(undefined);
  vi.stubGlobal("navigator", { clipboard: { writeText } });
  const call = vi.fn().mockResolvedValue({ path });
  const client = createQueryClient();
  const view = render(
    <APIProvider value={call as unknown as API}>
      <QueryClientProvider client={client}>
        <External id="file" />
      </QueryClientProvider>
    </APIProvider>,
  );
  await waitFor(() =>
    expect(
      screen.getByRole("link", { name: "用 IINA 打开" }).getAttribute("href"),
    ).toBe(`iina://open?url=${encodeURIComponent(url)}`),
  );
  expect(screen.queryByRole("textbox")).toBeNull();
  expect(
    screen.queryByText(/链接不过期|当前地址仅供本机使用|注册协议/),
  ).toBeNull();

  fireEvent.click(screen.getByRole("button", { name: "复制串流地址" }));
  await screen.findByText("已复制串流地址");
  expect(writeText).toHaveBeenCalledWith(url);
  expect(screen.queryByRole("textbox")).toBeNull();

  writeText.mockRejectedValueOnce(new Error("clipboard denied"));
  fireEvent.click(screen.getByRole("button", { name: "复制串流地址" }));
  const input = (await screen.findByRole("textbox")) as HTMLInputElement;
  expect(input.value).toBe(url);
  expect(input.readOnly).toBe(true);
  expect(document.activeElement).toBe(input);
  expect(input.selectionStart).toBe(0);
  expect(input.selectionEnd).toBe(url.length);

  fireEvent.click(screen.getByRole("button", { name: "复制串流地址" }));
  await waitFor(() => expect(screen.queryByRole("textbox")).toBeNull());
  view.unmount();
  client.clear();
});
