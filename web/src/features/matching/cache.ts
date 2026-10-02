import type { Query, QueryClient } from "@tanstack/react-query";
import type { Schema } from "../../shared/api";
export function matchingAffected(
  query: Query,
  subjectId: string,
  libraryId: string,
) {
  const resource = String(query.queryKey[0]);
  const params = query.queryKey[1] as { libraryId?: string } | undefined;
  if (["home", "groups", "person", "works"].includes(resource)) return true;
  if (resource === "catalog")
    return !params?.libraryId || params.libraryId === libraryId;
  if (resource === "media" || resource === "series") {
    const data = query.state.data as Schema["MediaDetail"] | undefined;
    return !data || data.match.subjectId === subjectId;
  }
  return resource === "episodes";
}
export async function confirmMatch(
  client: QueryClient,
  libraryId: string,
  match: Schema["Match"],
) {
  const predicate = (q: Query) =>
    matchingAffected(q, match.subjectId, libraryId);
  await client.cancelQueries({ predicate });
  for (const query of client.getQueryCache().findAll({ predicate })) {
    if (query.queryKey[0] !== "media" && query.queryKey[0] !== "series")
      continue;
    client.setQueryData(
      query.queryKey,
      (data: Schema["MediaDetail"] | Schema["SeriesDetail"] | undefined) =>
        data && data.match.subjectId === match.subjectId
          ? {
              ...data,
              match,
              // Card titles/episode labels remain server-owned projections;
              // refetch them instead of rebuilding display semantics here.
            }
          : data,
    );
  }
  await client.invalidateQueries({ predicate });
}
