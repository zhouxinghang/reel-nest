import { queryOptions } from "@tanstack/react-query";
import type { operations } from "../../generated/api";
import type { API } from "../../shared/api";

export type CatalogParams = NonNullable<
  operations["catalog"]["parameters"]["query"]
>;
export const keys = {
  session: ["session"] as const,
  home: (cursor = "") => ["home", { cursor }] as const,
  catalog: (params: CatalogParams) => ["catalog", params] as const,
  groups: (params: CatalogParams) => ["groups", params] as const,
  media: (id: string) => ["media", { id }] as const,
  series: (id: string, cursor = "") => ["series", { id, cursor }] as const,
  episodes: (id: string, season: number, cursor = "") =>
    ["episodes", { id, season, cursor }] as const,
  person: (id: string) => ["person", { id }] as const,
  works: (id: string, cursor = "") => ["works", { id, cursor }] as const,
  settings: ["settings"] as const,
  tasks: ["tasks"] as const,
};

// Page changes replace data instead of retaining an infinite query's page array.
// Inactive list pages are evicted immediately; back/forward refetches their URL.
export const queries = {
  home: (api: API, cursor = "") =>
    queryOptions({
      queryKey: keys.home(cursor),
      gcTime: 0,
      queryFn: ({ signal }) =>
        api("get", "/api/home", { query: { cursor }, signal }),
    }),
  catalog: (api: API, params: CatalogParams) =>
    queryOptions({
      queryKey: keys.catalog(params),
      gcTime: 0,
      queryFn: ({ signal }) =>
        api("get", "/api/catalog", { query: params, signal }),
    }),
  groups: (api: API, params: CatalogParams) =>
    queryOptions({
      queryKey: keys.groups(params),
      gcTime: 0,
      queryFn: ({ signal }) =>
        api("get", "/api/catalog/groups", { query: params, signal }),
    }),
  media: (api: API, id: string) =>
    queryOptions({
      queryKey: keys.media(id),
      queryFn: ({ signal }) =>
        api("get", "/api/media/{id}", { path: { id }, signal }),
    }),
  series: (api: API, id: string, cursor = "") =>
    queryOptions({
      queryKey: keys.series(id, cursor),
      gcTime: 0,
      queryFn: ({ signal }) =>
        api("get", "/api/series/{id}", {
          path: { id },
          query: { cursor, limit: 50 },
          signal,
        }),
    }),
  episodes: (api: API, id: string, season: number, cursor = "") =>
    queryOptions({
      queryKey: keys.episodes(id, season, cursor),
      gcTime: 0,
      queryFn: ({ signal }) =>
        api("get", "/api/series/{id}/seasons/{number}/episodes", {
          path: { id, number: season },
          query: { cursor, limit: 50 },
          signal,
        }),
    }),
  person: (api: API, id: string) =>
    queryOptions({
      queryKey: keys.person(id),
      queryFn: ({ signal }) =>
        api("get", "/api/people/{id}", { path: { id }, signal }),
    }),
  works: (api: API, id: string, cursor = "") =>
    queryOptions({
      queryKey: keys.works(id, cursor),
      gcTime: 0,
      queryFn: ({ signal }) =>
        api("get", "/api/people/{id}/works", {
          path: { id },
          query: { cursor, limit: 50 },
          signal,
        }),
    }),
  settings: (api: API) =>
    queryOptions({
      queryKey: keys.settings,
      queryFn: ({ signal }) => api("get", "/api/settings/metadata", { signal }),
    }),
};
