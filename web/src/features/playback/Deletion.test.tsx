import type { ReactNode } from "react";
import { QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { APIError, type API } from "../../shared/api";
import { APIProvider, createQueryClient } from "../../shared/services";
import { Deletion } from "./Deletion";
function setup(outcome: unknown, expiresAt = Date.now() + 60000) {
  HTMLDialogElement.prototype.showModal = vi.fn(function (
    this: HTMLDialogElement,
  ) {
    this.open = true;
  });
  HTMLDialogElement.prototype.close = vi.fn(function (this: HTMLDialogElement) {
    this.open = false;
  });
  const api = vi.fn(async (_method, path) => {
    if (path.endsWith("/deletion-preview"))
      return {
        mediaId: "file",
        path: "/tmp/controlled/only.webm",
        confirmation: "exact-version",
        expiresAt,
        warning: "永久删除",
      };
    if (outcome instanceof Error) throw outcome;
    return outcome;
  }) as unknown as API;
  const client = createQueryClient();
  const close = vi.fn();
  const wrapper = ({ children }: { children: ReactNode }) => (
    <APIProvider value={api}>
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    </APIProvider>
  );
  const view = render(
    <Deletion id="file" libraryId="lib" subjectId="subject" onClose={close} />,
    { wrapper },
  );
  return { api, client, close, view };
}
it("preview/cancel never deletes; expired confirmations cannot submit", async () => {
  const h = setup({}, Date.now() - 1);
  await screen.findByText("/tmp/controlled/only.webm");
  fireEvent.click(screen.getByRole("checkbox"));
  expect(
    (
      screen.getByRole("button", {
        name: "永久删除此文件",
      }) as HTMLButtonElement
    ).disabled,
  ).toBe(true);
  fireEvent.click(screen.getByRole("button", { name: "取消" }));
  expect(h.close).toHaveBeenCalledWith("");
  expect(h.api).toHaveBeenCalledTimes(1);
  h.view.unmount();
  h.client.clear();
});
it.each([
  [
    {
      fileOutcome: "deleted",
      indexOutcome: "pending",
      message: "核对索引",
      retryable: false,
      requestId: "r",
    },
    "索引更新未完成",
  ],
  [
    {
      fileOutcome: "unknown",
      indexOutcome: "pending",
      message: "核对文件",
      retryable: false,
      requestId: "r",
    },
    "删除结果未知",
  ],
  [new APIError("NETWORK_ERROR", "未确认"), "删除结果未知"],
])(
  "partial/unknown delete has a terminal non-retry result: %s",
  async (outcome, text) => {
    const h = setup(outcome);
    await screen.findByText("/tmp/controlled/only.webm");
    fireEvent.click(screen.getByRole("checkbox"));
    fireEvent.click(screen.getByRole("button", { name: "永久删除此文件" }));
    await screen.findByText(new RegExp(text as string));
    await waitFor(() => expect(h.api).toHaveBeenCalledTimes(2));
    expect(screen.queryByRole("button", { name: "永久删除此文件" })).toBeNull();
    expect(h.api).toHaveBeenLastCalledWith("post", "/api/media/{id}/delete", {
      path: { id: "file" },
      body: { confirmation: "exact-version", confirm: true },
    });
    h.view.unmount();
    h.client.clear();
  },
);
