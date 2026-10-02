import { queryOptions } from "@tanstack/react-query";
import type { API } from "../../shared/api";
export const librariesQuery = (api: API, cursor = "") =>
  queryOptions({
    queryKey: ["libraries", { cursor }],
    gcTime: 0,
    queryFn: ({ signal }) =>
      api("get", "/api/libraries", { query: { cursor, limit: 50 }, signal }),
  });
