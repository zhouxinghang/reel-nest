import type { CatalogParams } from "./queries";

export const categories = [
  "all",
  "movie",
  "series",
  "unmatched",
  "favorites",
  "recent",
  "continue",
] as const;
export type Category = (typeof categories)[number];
export const labels: Record<Category, string> = {
  all: "媒体库",
  movie: "电影",
  series: "电视剧",
  unmatched: "未匹配媒体",
  favorites: "收藏",
  recent: "最近添加",
  continue: "继续观看",
};
export function catalogState(search: URLSearchParams) {
  const raw = search.get("category");
  const category = categories.find((c) => c === raw) ?? "all";
  const view =
    category === "unmatched" ||
    category === "favorites" ||
    category === "recent" ||
    category === "continue"
      ? category
      : "browse";
  const params: CatalogParams = {
    libraryId: search.get("library") || undefined,
    kind: category === "movie" || category === "series" ? category : undefined,
    view,
    q: search.get("q") || undefined,
    sort:
      view === "favorites"
        ? "favorite"
        : view === "continue"
          ? "progress"
          : view === "recent" || search.get("sort") === "recent"
            ? "recent"
            : "title",
    limit: 50,
    cursor: search.get("cursor") || undefined,
  };
  return {
    category,
    params,
    layout: search.get("layout") === "list" ? "list" : "grid",
  } as const;
}
export function changeFilters(
  search: URLSearchParams,
  values: Record<string, string>,
) {
  const next = new URLSearchParams(search);
  for (const key of [...next.keys()]) {
    if (key === "cursor" || key.startsWith("group.")) next.delete(key);
  }
  for (const [key, value] of Object.entries(values)) {
    if (value) next.set(key, value);
    else next.delete(key);
  }
  return next;
}
export function changeCursor(
  search: URLSearchParams,
  key: string,
  value: string | null,
) {
  const next = new URLSearchParams(search);
  if (key === "cursor") {
    for (const name of [...next.keys()])
      if (name.startsWith("group.")) next.delete(name);
  }
  if (value) next.set(key, value);
  else next.delete(key);
  return next;
}
