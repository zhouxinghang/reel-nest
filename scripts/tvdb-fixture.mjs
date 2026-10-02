import { createServer } from "node:http";
import { readFileSync } from "node:fs";

// Only the external TVDB v4 API/artwork are replaced, never app internals.
export async function startTVDBFixture() {
  let mode = "success";
  const server = createServer((req, res) => {
    const url = new URL(req.url, "http://127.0.0.1:18094");
    if (url.pathname === "/control" && req.method === "POST") {
      mode = url.searchParams.get("mode");
      res.end("ok");
      return;
    }
    if (mode === "error") {
      res.writeHead(500);
      res.end("External TVDB unavailable");
      return;
    }
    if (url.pathname === "/artwork/types") {
      res.setHeader("Content-Type", "application/json");
      res.end(
        JSON.stringify({
          status: "success",
          data: [
            { id: 3, name: "Series Backgrounds", recordType: "series" },
            { id: 8, name: "Movie Backgrounds", recordType: "movie" },
          ],
        }),
      );
      return;
    }
    if (url.pathname.startsWith("/artwork/")) {
      if (req.headers.authorization) {
        res.writeHead(403);
        res.end();
        return;
      }
      res.writeHead(200, { "Content-Type": "image/png" });
      const landscape = url.pathname.endsWith("/thumb.png")
        ? "thumb"
        : url.pathname.endsWith("/backdrop.png")
          ? "backdrop"
          : "poster";
      res.end(readFileSync(`tests/fixtures/tmdb-${landscape}.png`));
      return;
    }
    res.setHeader("Content-Type", "application/json");
    if (url.pathname === "/login" && req.method === "POST") {
      let body = "";
      for (const event of ["error", "aborted"]) req.on(event, () => res.end());
      req.on("data", (chunk) => {
        body += chunk;
      });
      req.on("end", () => {
        const credentials = JSON.parse(body);
        if (credentials.apikey !== "isolated-browser-tvdb-key") {
          res.writeHead(401);
          res.end();
          return;
        }
        res.end(
          JSON.stringify({
            status: "success",
            data: { token: "isolated-browser-tvdb-jwt" },
          }),
        );
      });
      return;
    }
    if (req.headers.authorization !== "Bearer isolated-browser-tvdb-jwt") {
      res.writeHead(401);
      res.end();
      return;
    }
    const series =
      url.searchParams.get("type") === "series" ||
      url.pathname.includes("/series/");
    const name = series ? "Dark" : "Arrival";
    const title = series ? "TVDB 暗黑" : "TVDB 降临";
    const year = series ? "2017" : "2016";
    if (
      url.pathname.endsWith("/extended") &&
      url.searchParams.get("short") === "false"
    ) {
      if (mode === "credits-error") {
        res.writeHead(500);
        res.end("Credits unavailable");
        return;
      }
      const characters = [
        ...Array.from({ length: 10 }, (_, i) => ({
          peopleId: 100 + i,
          personName:
            i === 0
              ? "TVDB 主演"
              : i === 8
                ? "一位拥有很长很长名字的演员·LongNameWithoutSpacesForMobileLayout"
                : `TVDB 演员 ${i + 1}`,
          name: i === 0 ? "语言学家" : "角色",
          peopleType: "Actor",
          sort: i,
          personImgURL:
            i < 2 ? "https://artworks.thetvdb.com/banners/person.png" : null,
        })),
        {
          peopleId: 201,
          personName: "TVDB 导演",
          personImgURL: "https://artworks.thetvdb.com/banners/director.png",
          name: "Director",
          peopleType: "Director",
        },
        { peopleId: 202, personName: "非导演编剧", peopleType: "Writer" },
      ];
      res.end(
        JSON.stringify({
          status: "success",
          data: {
            id: 10,
            name,
            year,
            image: "https://artworks.thetvdb.com/banners/poster.png",
            translations: {
              nameTranslations: [{ language: "zho", name: title }],
              overviewTranslations: [
                { language: "zho", overview: "TVDB 中文隔离测试资料。" },
              ],
            },
            characters: mode === "credits-empty" ? [] : characters,
            artworks: [
              {
                type: series ? 3 : 8,
                image: "https://artworks.thetvdb.com/banners/thumb.png",
                includesText: true,
                language: "zho",
                width: 640,
                height: 360,
                score: 8,
              },
              {
                type: series ? 3 : 8,
                image: "https://artworks.thetvdb.com/banners/backdrop.png",
                includesText: false,
                language: "",
                width: 640,
                height: 360,
                score: 9,
              },
            ],
          },
        }),
      );
      return;
    }
    const data =
      url.pathname === "/search"
        ? [
            {
              tvdb_id: "10",
              name,
              year,
              type: series ? "series" : "movie",
              translations: { zho: title },
            },
          ]
        : {
            id: 10,
            name,
            year,
            image: "https://artworks.thetvdb.com/banners/poster.png",
            translations: {
              nameTranslations: [{ language: "zho", name: title }],
              overviewTranslations: [
                {
                  language: "zho",
                  overview: "TVDB 中文隔离测试资料，不是真实资料。",
                },
              ],
            },
          };
    res.end(JSON.stringify({ status: "success", data, links: { next: null } }));
  });
  await new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(18094, "127.0.0.1", resolve);
  });
  return server;
}
