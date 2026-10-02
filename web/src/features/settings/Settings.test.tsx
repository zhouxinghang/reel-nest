import type { ReactNode } from "react";
import { QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { APIError, type API } from "../../shared/api";
import { APIProvider, createQueryClient } from "../../shared/services";
import { provider } from "../../shared/test-fixtures";
import { ProviderForm } from "./Settings";

describe("revision-safe settings draft", () => {
  it("keeps drafts across background updates and conflict; explicit reload updates only baseline", async () => {
    const call = vi
      .fn()
      .mockRejectedValueOnce(
        new APIError("REVISION_CONFLICT", "版本已变化", 409),
      )
      .mockResolvedValueOnce({
        sources: [{ ...provider, url: "http://other/v1", revision: 2 }],
      })
      .mockResolvedValueOnce({
        ...provider,
        url: "http://draft/v1",
        revision: 3,
        credentialSet: true,
      });
    const api = call as unknown as API;
    const client = createQueryClient();
    const wrapper = ({ children }: { children: ReactNode }) => (
      <APIProvider value={api}>
        <QueryClientProvider client={client}>{children}</QueryClientProvider>
      </APIProvider>
    );
    const view = render(<ProviderForm initial={provider} />, { wrapper });
    const url = screen.getByLabelText("服务地址") as HTMLInputElement;
    const token = screen.getByLabelText("Token") as HTMLInputElement;
    fireEvent.change(url, { target: { value: "http://draft/v1" } });
    fireEvent.change(token, { target: { value: "draft-secret" } });
    view.rerender(
      <ProviderForm
        initial={{ ...provider, revision: 2, url: "http://background/v1" }}
      />,
    );
    expect(url.value).toBe("http://draft/v1");
    fireEvent.click(screen.getByRole("button", { name: "保存 MetaTube" }));
    await screen.findByText(/其他会话修改了配置/);
    expect(token.value).toBe("draft-secret");
    expect(call).toHaveBeenCalledTimes(1);
    expect(call.mock.calls[0][2].body.revision).toBe(1);
    expect(
      (
        screen.getByRole("button", {
          name: "保存 MetaTube",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
    fireEvent.click(
      screen.getByRole("button", { name: "读取最新版本，保留草稿" }),
    );
    await screen.findByText(/已读取版本 2/);
    expect(url.value).toBe("http://draft/v1");
    expect(token.value).toBe("draft-secret");
    fireEvent.click(screen.getByRole("button", { name: "保存 MetaTube" }));
    await waitFor(() => expect(call).toHaveBeenCalledTimes(3));
    expect(call.mock.calls[2][2].body).toMatchObject({
      revision: 2,
      url: "http://draft/v1",
      credential: "draft-secret",
    });
    await screen.findByText(/设置已保存/);
    expect(token.value).toBe("");
    view.unmount();
    client.clear();
  });
});
