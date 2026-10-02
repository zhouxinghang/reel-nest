import { describe, expect, it, vi } from "vitest";
import { confirmFavorite, favoriteAffected, mapCards } from "./cache";
import { keys, queries } from "./queries";
import { createQueryClient } from "../../shared/services";
import { card, deferred, page } from "../../shared/test-fixtures";
import type { API, Schema } from "../../shared/api";

describe("bounded query cache", () => {
  it("patches all bounded card projections with server-confirmed favorite", () => {
    const favorite = { favorite: true, favoritedAt: 2000 };
    const home: Schema["Home"] = {
      libraries: [
        { library: { id: "lib1", name: "库", kind: "movie" }, items: [card] },
      ],
      librariesPage: page,
      recent: [card],
      continueWatching: [card],
    };
    const result = mapCards("home", home, (c) => ({
      ...c,
      favorite,
    })) as Schema["Home"];
    expect(result.libraries[0].items[0].favorite).toEqual(favorite);
    expect(result.recent[0].favorite).toEqual(favorite);
    expect(result.continueWatching[0].favorite).toEqual(favorite);
    expect(home.recent[0].favorite.favorite).toBe(false);
  });
  it("cancels reads started during PUT so late stale responses cannot undo confirmation", async () => {
    const client = createQueryClient();
    const key = keys.catalog({ libraryId: card.libraryId, limit: 50 });
    const oldPage: Schema["CatalogPage"] = { items: [card], page };
    client.setQueryData(key, oldPage);
    const old = deferred<Schema["CatalogPage"]>();
    let signal: AbortSignal | undefined;
    const read = client
      .fetchQuery({
        queryKey: key,
        staleTime: 0,
        queryFn: (context) => {
          signal = context.signal;
          return old.promise;
        },
      })
      .catch(() => undefined);
    const other = keys.catalog({ libraryId: "other", limit: 50 });
    client.setQueryData(other, oldPage);
    const unrelated = client.getQueryCache().find({ queryKey: other })!;
    expect(favoriteAffected(unrelated, card)).toBe(false);
    await confirmFavorite(client, card, { favorite: true, favoritedAt: 2000 });
    expect(signal?.aborted).toBe(true);
    old.resolve(oldPage);
    await read;
    expect(
      client.getQueryData<Schema["CatalogPage"]>(key)?.items[0].favorite
        .favorite,
    ).toBe(true);
    expect(client.getQueryState(other)?.isInvalidated).toBe(false);
    client.clear();
  });
  it("invalidates favorite membership only; unrelated state/resources stay fresh", async () => {
    const client = createQueryClient();
    const favorites = keys.catalog({ view: "favorites", libraryId: "lib1" });
    const ordinary = keys.catalog({ view: "browse", libraryId: "lib1" });
    client.setQueryData(favorites, { items: [], page });
    client.setQueryData(ordinary, { items: [card], page });
    client.setQueryData(keys.settings, { sources: [] });
    await confirmFavorite(client, card, { favorite: true, favoritedAt: 2000 });
    expect(client.getQueryState(favorites)?.isInvalidated).toBe(true);
    expect(client.getQueryState(ordinary)?.isInvalidated).toBe(false);
    expect(client.getQueryState(keys.settings)?.isInvalidated).toBe(false);
    client.clear();
  });
  it("list factories pass AbortSignal and evict unobserved pages rather than accumulating arrays", async () => {
    const api = vi
      .fn()
      .mockResolvedValue({ items: [], page }) as unknown as API;
    const config = queries.catalog(api, { limit: 50, cursor: "page2" });
    expect(config.gcTime).toBe(0);
    const client = createQueryClient();
    await client.fetchQuery(config);
    expect(api).toHaveBeenCalledWith(
      "get",
      "/api/catalog",
      expect.objectContaining({
        query: { limit: 50, cursor: "page2" },
        signal: expect.any(AbortSignal),
      }),
    );
    await new Promise((done) => setTimeout(done, 1));
    expect(client.getQueryState(config.queryKey)).toBeUndefined();
    client.clear();
  });
});
