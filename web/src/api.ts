export class APIError extends Error {
  constructor(
    message: string,
    public status: number,
  ) {
    super(message);
  }
}

export async function api<T>(path: string, body?: unknown): Promise<T> {
  const response = await fetch(path, {
    method: body === undefined ? "GET" : "POST",
    credentials: "same-origin",
    headers: body === undefined ? {} : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const result = await response.json();
  if (!response.ok) {
    if (
      response.status === 401 &&
      path !== "/api/login" &&
      window.location.pathname !== "/login"
    ) {
      window.location.replace("/login");
    }
    throw new APIError(result.error || "请求失败，请重试", response.status);
  }
  return result as T;
}

export function message(error: unknown): string {
  return error instanceof Error ? error.message : "请求失败，请重试";
}
