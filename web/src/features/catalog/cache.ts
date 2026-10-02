import type { Query, QueryClient } from "@tanstack/react-query";
import type { Schema } from "../../shared/api";

type Target = Pick<Schema["Card"], "entryType" | "id" | "libraryId">;
const cardResources = new Set([
  "home",
  "catalog",
  "media",
  "series",
  "episodes",
  "works",
]);

// Walk only the already bounded DTOs, not arbitrary application/server objects.
export function mapCards(
  resource: string,
  data: unknown,
  update: (card: Schema["Card"]) => Schema["Card"],
): unknown {
  if (!data) return data;
  switch (resource) {
    case "home": {
      const d = data as Schema["Home"];
      return {
        ...d,
        libraries: d.libraries.map((l) => ({
          ...l,
          items: l.items.map(update),
        })),
        recent: d.recent.map(update),
        continueWatching: d.continueWatching.map((c) => update(c)),
      };
    }
    case "catalog": {
      const d = data as Schema["CatalogPage"];
      return { ...d, items: d.items.map(update) };
    }
    case "media":
    case "series": {
      const d = data as Schema["MediaDetail"] | Schema["SeriesDetail"];
      return { ...d, card: update(d.card) };
    }
    case "episodes": {
      const d = data as Schema["EpisodePage"];
      return {
        ...d,
        items: d.items.map((e) => ({ ...e, file: update(e.file) })),
      };
    }
    case "works": {
      const d = data as Schema["PersonWorks"];
      return {
        ...d,
        items: d.items.map((w) => ({ ...w, card: update(w.card) })),
      };
    }
    default:
      return data;
  }
}
export function favoriteAffected(query: Query, target: Target) {
  const resource = String(query.queryKey[0]);
  const params = query.queryKey[1] as
    | { id?: string; libraryId?: string; view?: string }
    | undefined;
  if (resource === "home" || resource === "works" || resource === "groups")
    return true;
  if (resource === "catalog")
    return !params?.libraryId || params.libraryId === target.libraryId;
  if (resource === "media" || resource === "series")
    return resource === target.entryType && params?.id === target.id;
  // A file can occur in any already-loaded episode page (including a pending read).
  if (resource === "episodes") return target.entryType === "media";
  return false;
}
export async function confirmFavorite(
  client: QueryClient,
  target: Target,
  favorite: Schema["Favorite"],
) {
  // Cancel again at confirmation: reads may have started while PUT was in flight.
  await client.cancelQueries({ predicate: (q) => favoriteAffected(q, target) });
  for (const query of client
    .getQueryCache()
    .findAll({ predicate: (q) => favoriteAffected(q, target) })) {
    client.setQueryData(query.queryKey, (data: unknown) =>
      mapCards(String(query.queryKey[0]), data, (card) =>
        card.entryType === target.entryType && card.id === target.id
          ? { ...card, favorite }
          : card,
      ),
    );
  }
  await client.invalidateQueries({
    predicate: (q) => favoriteAffected(q, target) && q.state.data === undefined,
  });
  // Only membership/order of favorites pages changes; ordinary pages keep their cursor.
  await client.invalidateQueries({
    predicate: (q) =>
      (q.queryKey[0] === "catalog" || q.queryKey[0] === "groups") &&
      favoriteAffected(q, target) &&
      (q.queryKey[1] as { view?: string })?.view === "favorites",
  });
}
export function invalidateMetadata(client: QueryClient) {
  // Status has a global revision, not changed subject IDs. Refresh only subscribed
  // bounded projections; do not fetch every library or every cached detail.
  return client.invalidateQueries({
    predicate: (q) =>
      cardResources.has(String(q.queryKey[0])) ||
      q.queryKey[0] === "groups" ||
      q.queryKey[0] === "person",
  });
}
