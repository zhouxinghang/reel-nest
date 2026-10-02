import type { components, paths } from "../generated/api";

export type Schema = components["schemas"];
type Method = "get" | "post" | "put" | "patch";
type Endpoint<M extends Method> = {
  [P in keyof paths]: paths[P][M] extends { responses: unknown } ? P : never;
}[keyof paths];
type Operation<M extends Method, P extends Endpoint<M>> = NonNullable<
  paths[P][M]
>;
type Parameters<O> = O extends { parameters: infer P } ? P : never;
type PathArgs<O> =
  Parameters<O> extends { path: infer P } ? { path: P } : { path?: never };
type QueryArgs<O> =
  Parameters<O> extends { query: infer Q }
    ? { query: Q }
    : Parameters<O> extends { query?: infer Q }
      ? { query?: Q }
      : { query?: never };
type BodyArgs<O> = O extends {
  requestBody: { content: { "application/json": infer B } };
}
  ? { body: B }
  : { body?: never };
type Result<O> = O extends { responses: infer R }
  ? {
      [S in keyof R]: S extends 200 | 201 | 202
        ? R[S] extends { content: { "application/json": infer D } }
          ? D
          : R[S] extends { content: { "text/vtt": string } }
            ? string
            : never
        : never;
    }[keyof R]
  : never;
type Options<O> = PathArgs<O> &
  QueryArgs<O> &
  BodyArgs<O> & {
    signal?: AbortSignal;
    timeoutMs?: number;
  };

type ClientCode = "TIMEOUT" | "NETWORK_ERROR" | "INVALID_RESPONSE";
export class APIError extends Error {
  constructor(
    public code: Schema["ErrorCode"] | ClientCode,
    message: string,
    public status = 0,
    public retryable = false,
    public requestId: string | null = null,
  ) {
    super(message);
    this.name = "APIError";
  }
}
export function isCancelled(error: unknown) {
  return (
    !!error &&
    typeof error === "object" &&
    "name" in error &&
    error.name === "AbortError"
  );
}
export function errorMessage(error: unknown) {
  if (error == null || isCancelled(error)) return "";
  return error instanceof APIError ? error.message : "请求失败，请稍后重试";
}
function errorEnvelope(value: unknown): value is Schema["Error"] {
  if (!value || typeof value !== "object" || !("error" in value)) return false;
  const e = value.error;
  return (
    !!e &&
    typeof e === "object" &&
    "code" in e &&
    typeof e.code === "string" &&
    "message" in e &&
    typeof e.message === "string" &&
    "retryable" in e &&
    typeof e.retryable === "boolean" &&
    "requestId" in e &&
    typeof e.requestId === "string"
  );
}

// One client per application; no window events, hidden global session, or retries.
// The Router owner handles 401 and can clear the entire household query cache.
export function createAPI(
  onUnauthorized: () => void,
  fetcher: typeof fetch = fetch,
) {
  const requests = new Set<AbortController>();
  async function request<M extends Method, P extends Endpoint<M>>(
    method: M,
    endpoint: P,
    options: Options<Operation<M, P>>,
  ): Promise<Result<Operation<M, P>>> {
    const controller = new AbortController();
    requests.add(controller);
    const abort = () => controller.abort();
    if (options.signal?.aborted) abort();
    options.signal?.addEventListener("abort", abort, { once: true });
    let timedOut = false;
    const timer = setTimeout(() => {
      timedOut = true;
      controller.abort();
    }, options.timeoutMs ?? 15000);
    try {
      controller.signal.throwIfAborted();
      let url: string = endpoint;
      for (const [key, value] of Object.entries(options.path ?? {})) {
        url = url.replace(`{${key}}`, encodeURIComponent(String(value)));
      }
      const query = new URLSearchParams();
      for (const [key, value] of Object.entries(options.query ?? {})) {
        if (value !== undefined && value !== null && value !== "")
          query.set(key, String(value));
      }
      if (query.size) url += `?${query}`;
      const response = await fetcher(url, {
        method: method.toUpperCase(),
        credentials: "same-origin",
        headers: {
          Accept: "application/json",
          ...(options.body !== undefined
            ? { "Content-Type": "application/json" }
            : {}),
        },
        body:
          options.body === undefined ? undefined : JSON.stringify(options.body),
        signal: controller.signal,
        redirect: "error",
      });
      // Check cancellation after every await as well (including controlled test transports).
      controller.signal.throwIfAborted();
      if (response.status === 401 && endpoint !== "/api/login")
        onUnauthorized();
      if (response.ok && endpoint === "/api/media/{id}/subtitles/{subtitle}") {
        const body = await response.text();
        controller.signal.throwIfAborted();
        if (
          !response.headers.get("content-type")?.includes("text/vtt") ||
          !body.startsWith("WEBVTT")
        )
          throw new APIError(
            "INVALID_RESPONSE",
            "字幕响应格式异常",
            response.status,
          );
        return body as Result<Operation<M, P>>;
      }
      let data: unknown;
      try {
        data = await response.json();
      } catch {
        controller.signal.throwIfAborted();
        throw new APIError(
          "INVALID_RESPONSE",
          `服务器返回了非 JSON 响应（${response.status}）`,
          response.status,
          response.status >= 500,
        );
      }
      controller.signal.throwIfAborted();
      if (!response.ok) {
        if (errorEnvelope(data)) {
          const e = data.error;
          throw new APIError(
            e.code,
            e.message,
            response.status,
            e.retryable,
            e.requestId,
          );
        }
        throw new APIError(
          "INVALID_RESPONSE",
          `服务器响应格式异常（${response.status}）`,
          response.status,
        );
      }
      return data as Result<Operation<M, P>>;
    } catch (error) {
      if (options.signal?.aborted)
        throw new DOMException("请求已取消", "AbortError");
      if (timedOut)
        throw new APIError("TIMEOUT", "请求超时，操作结果尚未确认", 0, true);
      if (error instanceof APIError || isCancelled(error)) throw error;
      throw new APIError(
        "NETWORK_ERROR",
        "网络连接失败，操作结果尚未确认",
        0,
        true,
      );
    } finally {
      requests.delete(controller);
      clearTimeout(timer);
      options.signal?.removeEventListener("abort", abort);
    }
  }
  return Object.assign(request, {
    cancelAll() {
      for (const controller of requests) controller.abort();
    },
  });
}
export type API = ReturnType<typeof createAPI>;
