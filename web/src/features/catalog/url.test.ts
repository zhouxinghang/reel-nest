import { describe, expect, it } from "vitest";
import { catalogState, changeCursor, changeFilters } from "./url";
import { safeReturnPath } from "../identity/Login";

describe("URL-owned browsing", () => {
  it("round trips library/category/search/sort/layout/cursor without client sorting", () => {
    const search = new URLSearchParams({
      library: "l1",
      category: "series",
      q: "中文 12",
      sort: "recent",
      layout: "list",
      cursor: "opaque",
    });
    expect(catalogState(new URLSearchParams(search.toString()))).toEqual({
      category: "series",
      layout: "list",
      params: {
        libraryId: "l1",
        kind: "series",
        view: "browse",
        q: "中文 12",
        sort: "recent",
        limit: 50,
        cursor: "opaque",
      },
    });
  });
  it("uses server event sorting and bounded defaults, ignoring invalid enums/limits", () => {
    expect(
      catalogState(
        new URLSearchParams("category=favorites&sort=title&limit=999999"),
      ).params,
    ).toMatchObject({ sort: "favorite", limit: 50 });
    expect(
      catalogState(new URLSearchParams("category=continue")).params.sort,
    ).toBe("progress");
    expect(
      catalogState(
        new URLSearchParams("category=unknown&sort=hack&layout=hack"),
      ),
    ).toMatchObject({
      category: "all",
      layout: "grid",
      params: { sort: "title" },
    });
  });
  it("filter changes reset all cursors while child page changes stay independent", () => {
    const initial = new URLSearchParams(
      "q=a&cursor=g2&group.a=a2&group.b=b3&layout=list",
    );
    expect(changeCursor(initial, "group.a", "a3").get("group.b")).toBe("b3");
    expect(changeCursor(initial, "cursor", "g3").has("group.b")).toBe(false);
    expect(changeFilters(initial, { q: "b" }).toString()).toBe(
      "q=b&layout=list",
    );
    expect(initial.get("q")).toBe("a");
  });
  it("prevents external login return destinations", () => {
    for (const path of ["https://evil", "//evil", "/\\evil", "/login?next=/"])
      expect(safeReturnPath(path)).toBe("/");
    expect(safeReturnPath("/library?q=a&layout=list")).toBe(
      "/library?q=a&layout=list",
    );
  });
});
