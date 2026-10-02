import { QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup } from "@testing-library/react";
import { APIError, type API, type Schema } from "../../shared/api";
import { APIProvider, createQueryClient } from "../../shared/services";
import { provider } from "../../shared/test-fixtures";
import { Actors } from "./Actors";

afterEach(cleanup);
const candidate: Schema["ActorCandidate"] = {
  id: "actor.signature",
  name: "演员甲",
  aliases: ["别名甲"],
  provider: "example",
  photoId: "photo.signature",
};
const detail: Schema["ActorDetail"] = {
  ...candidate,
  summary: "演员的简介",
  homepage: "https://example.invalid/actor",
  birthday: "1995-01-02",
  debutDate: "",
  nationality: "",
  height: "170",
  bloodType: "",
  hobby: "阅读",
  skill: "",
  measurements: "",
  cupSize: "",
  wallpapers: ["image.signature"],
};
function setup(call: ReturnType<typeof vi.fn>, path = "/actors") {
  const client = createQueryClient();
  const view = render(
    <APIProvider value={call as unknown as API}>
      <QueryClientProvider client={client}>
        <MemoryRouter initialEntries={[path]}>
          <Actors />
        </MemoryRouter>
      </QueryClientProvider>
    </APIProvider>,
  );
  return () => {
    view.unmount();
    client.clear();
  };
}

describe("actor name queries", () => {
  it("cancels an old name query and never replaces new results with its late reply", async () => {
    let finish!: (value: Schema["ActorSearchPage"]) => void;
    let oldSignal: AbortSignal | undefined;
    const pending = new Promise<Schema["ActorSearchPage"]>((resolve) => {
      finish = resolve;
    });
    const call = vi.fn(async (_method, endpoint, options) => {
      if (endpoint === "/api/settings/metadata")
        return { sources: [{ ...provider, configured: true }] };
      if (options.query.q === "旧名字") {
        oldSignal = options.signal;
        return pending;
      }
      return { items: [{ ...candidate, name: "新演员" }], truncated: false };
    });
    const close = setup(call, "/actors?q=旧名字");
    await waitFor(() => expect(oldSignal).toBeDefined());
    fireEvent.change(screen.getByLabelText("演员名字"), {
      target: { value: "新名字" },
    });
    fireEvent.submit(screen.getByRole("search"));
    await screen.findByText("新演员");
    expect(oldSignal?.aborted).toBe(true);
    finish({ items: [{ ...candidate, name: "旧演员" }], truncated: false });
    await pending;
    expect(screen.queryByText("旧演员")).toBeNull();
    expect(screen.getByText("新演员")).toBeTruthy();
    close();
  });

  it("guides unconfigured users without sending actor queries", async () => {
    const call = vi
      .fn()
      .mockResolvedValue({ sources: [{ ...provider, configured: false }] });
    const close = setup(call, "/actors?q=演员甲");
    await screen.findByText(/尚未配置 MetaTube/);
    expect(call).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole("search")).toBeNull();
    close();
  });

  it("searches submitted names, selects explicit identity and keeps details on image failure", async () => {
    const call = vi.fn(async (_method, endpoint) => {
      if (endpoint === "/api/settings/metadata")
        return { sources: [{ ...provider, configured: true }] };
      if (endpoint === "/api/metadata/actors/search")
        return {
          items: [
            candidate,
            { ...candidate, id: "other.signature", provider: "other" },
          ],
          truncated: false,
        };
      if (endpoint === "/api/metadata/actors/{id}") return detail;
      throw new Error(endpoint);
    });
    const close = setup(call);
    const input = await screen.findByLabelText("演员名字");
    fireEvent.change(input, { target: { value: " 演员甲 " } });
    expect(call).toHaveBeenCalledTimes(1);
    fireEvent.submit(screen.getByRole("search"));
    await screen.findByText("找到 2 个候选，请选择演员");
    expect(call.mock.calls[1]).toMatchObject([
      "get",
      "/api/metadata/actors/search",
      { query: { q: "演员甲" } },
    ]);
    expect(screen.queryByRole("region", { name: "演员资料" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: /演员甲.*example/ }));
    await screen.findByText("演员的简介");
    expect(screen.getByText("170 cm")).toBeTruthy();
    const wallpaper = screen.getByAltText("演员甲壁纸 1");
    fireEvent.error(wallpaper);
    expect(screen.getByText("图片暂不可用")).toBeTruthy();
    expect(screen.getByText("演员的简介")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "重试图片" }));
    expect(screen.getByAltText("演员甲壁纸 1")).toBeTruthy();
    expect(
      screen.getByRole("link", { name: "下载图片" }).getAttribute("href"),
    ).toBe("/api/metadata/actor-images/image.signature");
    close();
  });

  it("distinguishes provider failure from no results and retries without losing the name", async () => {
    let attempts = 0;
    const call = vi.fn(async (_method, endpoint) => {
      if (endpoint === "/api/settings/metadata")
        return { sources: [{ ...provider, configured: true }] };
      if (++attempts === 1)
        throw new APIError("PROVIDER_UNAVAILABLE", "资料源暂不可用", 503, true);
      return { items: [], truncated: false };
    });
    const close = setup(call, "/actors?q=演员甲");
    await screen.findByText("资料源暂不可用");
    expect(screen.queryByText(/未找到演员/)).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "重试读取" }));
    await screen.findByText(/未找到演员/);
    expect((screen.getByLabelText("演员名字") as HTMLInputElement).value).toBe(
      "演员甲",
    );
    await waitFor(() => expect(attempts).toBe(2));
    close();
  });
});
