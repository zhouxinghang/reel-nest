import { afterEach, describe, expect, it, vi } from "vitest";
import { APIError, createAPI, errorMessage, isCancelled } from "./api";
import { deferred } from "./test-fixtures";

afterEach(() => vi.useRealTimers());
describe("generated-contract request client", () => {
  it("encodes path/query, uses exact method/body and same-origin credentials", async () => {
    const fetcher = vi
      .fn<typeof fetch>()
      .mockImplementation(async () =>
        Response.json({ favorite: true, favoritedAt: 1000 }),
      );
    const api = createAPI(vi.fn(), fetcher);
    const result = await api("put", "/api/favorites/{target}/{id}", {
      path: { target: "media", id: "x /中文#?" },
      body: { favorite: true },
    });
    expect(fetcher.mock.calls[0][0]).toBe(
      "/api/favorites/media/x%20%2F%E4%B8%AD%E6%96%87%23%3F",
    );
    expect(fetcher.mock.calls[0][1]).toMatchObject({
      method: "PUT",
      credentials: "same-origin",
      body: '{"favorite":true}',
      redirect: "error",
    });
    expect(result.favorite).toBe(true);
    await api("get", "/api/catalog", {
      query: { q: "中文 & #", libraryId: "lib", cursor: "", limit: 50 },
    });
    expect(
      new URL(
        String(fetcher.mock.calls[1][0]),
        "http://local",
      ).searchParams.get("q"),
    ).toBe("中文 & #");
    expect(String(fetcher.mock.calls[1][0])).not.toContain("cursor");
  });
  it("preserves code, requestId and retryability instead of inspecting messages", async () => {
    const api = createAPI(
      vi.fn(),
      vi.fn<typeof fetch>().mockResolvedValue(
        Response.json(
          {
            error: {
              code: "CURSOR_STALE",
              message: "重新加载",
              retryable: false,
              requestId: "req1",
            },
          },
          { status: 409 },
        ),
      ),
    );
    await expect(api("get", "/api/home", {})).rejects.toMatchObject({
      code: "CURSOR_STALE",
      status: 409,
      retryable: false,
      requestId: "req1",
    });
  });
  it("does not echo HTML/proxy response bodies", async () => {
    const api = createAPI(
      vi.fn(),
      vi
        .fn<typeof fetch>()
        .mockResolvedValue(
          new Response("<html>private upstream dump</html>", { status: 502 }),
        ),
    );
    await expect(api("get", "/api/home", {})).rejects.toMatchObject({
      code: "INVALID_RESPONSE",
      message: "服务器返回了非 JSON 响应（502）",
    });
  });
  it("handles 401 even with non-JSON body, but bad login does not redirect", async () => {
    const unauthorized = vi.fn();
    const fetcher = vi
      .fn<typeof fetch>()
      .mockImplementation(async () => new Response("", { status: 401 }));
    const api = createAPI(unauthorized, fetcher);
    await expect(api("get", "/api/session", {})).rejects.toBeInstanceOf(
      APIError,
    );
    expect(unauthorized).toHaveBeenCalledTimes(1);
    await expect(
      api("post", "/api/login", {
        body: { username: "family", password: "bad" },
      }),
    ).rejects.toBeInstanceOf(APIError);
    expect(unauthorized).toHaveBeenCalledTimes(1);
  });
  it("pre-aborted and mid-flight cancellation is silent and never triggers 401", async () => {
    const request = deferred<Response>();
    const fetcher = vi.fn<typeof fetch>().mockReturnValue(request.promise);
    const unauthorized = vi.fn();
    const api = createAPI(unauthorized, fetcher);
    const controller = new AbortController();
    controller.abort();
    await expect(
      api("get", "/api/home", { signal: controller.signal }),
    ).rejects.toMatchObject({ name: "AbortError" });
    expect(fetcher).not.toHaveBeenCalled();
    const next = new AbortController();
    const result = api("get", "/api/home", { signal: next.signal }).catch(
      (e: unknown) => e,
    );
    next.abort();
    request.resolve(new Response("", { status: 401 }));
    const error = await result;
    expect(isCancelled(error)).toBe(true);
    expect(errorMessage(error)).toBe("");
    expect(unauthorized).not.toHaveBeenCalled();
    expect(fetcher.mock.calls[0][1]?.signal?.aborted).toBe(true);
  });
  it("timeouts include body reading, clean timers and do not become cancellation", async () => {
    vi.useFakeTimers();
    const fetcher = vi.fn<typeof fetch>().mockImplementation(
      async (_, init) =>
        new Response(
          new ReadableStream({
            start(controller) {
              init?.signal?.addEventListener("abort", () =>
                controller.error(new DOMException("aborted", "AbortError")),
              );
            },
          }),
        ),
    );
    const result = createAPI(vi.fn(), fetcher)("get", "/api/home", {
      timeoutMs: 30,
    }).catch((e: unknown) => e);
    await vi.advanceTimersByTimeAsync(31);
    expect(await result).toMatchObject({ code: "TIMEOUT", retryable: true });
    expect(vi.getTimerCount()).toBe(0);
  });
  it("network failures are classified without automatic replay", async () => {
    const fetcher = vi
      .fn<typeof fetch>()
      .mockRejectedValue(new TypeError("sensitive transport detail"));
    await expect(
      createAPI(vi.fn(), fetcher)("post", "/api/media/{id}/delete", {
        path: { id: "a" },
        body: { confirmation: "signed", confirm: true },
      }),
    ).rejects.toMatchObject({ code: "NETWORK_ERROR" });
    expect(fetcher).toHaveBeenCalledTimes(1);
  });
});
// Compiled by typecheck:tests, never executed. Contract drift must break this gate.
function contractTypes(api: ReturnType<typeof createAPI>) {
  // @ts-expect-error progress uses PUT, not POST
  void api("post", "/api/media/{id}/progress", { path: { id: "a" }, body: {} });
  void api("post", "/api/media/{id}/delete", {
    path: { id: "a" },
    // @ts-expect-error delete requires confirm:true (no legacy confirmed field)
    body: { confirmed: true },
  });
  // @ts-expect-error candidate query is mandatory
  void api("get", "/api/metadata/subjects/{id}/candidates", {
    path: { id: "a" },
  });
}
void contractTypes;

describe("session boundary cancellation", () => {
  it("aborts pending writes as well as queries before the household cache is cleared", async () => {
    const pending = deferred<Response>();
    const unauthorized = vi.fn();
    const fetcher = vi
      .fn<typeof fetch>()
      .mockReturnValueOnce(pending.promise)
      .mockImplementation(async () => Response.json({ username: "family" }));
    const api = createAPI(unauthorized, fetcher);
    const write = api("put", "/api/favorites/{target}/{id}", {
      path: { target: "media", id: "a" },
      body: { favorite: true },
    }).catch((e: unknown) => e);
    api.cancelAll();
    pending.resolve(Response.json({ favorite: true, favoritedAt: 1000 }));
    expect(isCancelled(await write)).toBe(true);
    expect(await api("get", "/api/session", {})).toEqual({
      username: "family",
    });
    expect(unauthorized).not.toHaveBeenCalled();
  });
  it("does not manufacture alerts for null state or a canceled request", () => {
    expect(errorMessage(null)).toBe("");
    expect(errorMessage(undefined)).toBe("");
    expect(errorMessage(new DOMException("cancel", "AbortError"))).toBe("");
  });
});
