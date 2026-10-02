import { createServer } from "node:http";
import { readFileSync } from "node:fs";

// External service fixture. No application endpoints, DB hooks or media mocks.
export async function startTMDBFixture() {
  let mode = "success";
  const server = createServer((req, res) => {
    const url = new URL(req.url, "http://127.0.0.1:18092");
    if (url.pathname === "/control" && req.method === "POST") {
      mode = url.searchParams.get("mode");
      res.end("ok");
      return;
    }
    if (mode === "error") {
      res.writeHead(500);
      res.end("External service unavailable");
      return;
    }
    if (url.pathname.startsWith("/images/")) {
      if (
        mode === "artwork-image-error" &&
        !url.pathname.endsWith("/poster.png")
      ) {
        res.writeHead(404);
        res.end();
        return;
      }
      if (mode === "photo-error" && url.pathname.endsWith("/person.png")) {
        res.writeHead(404);
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
    if (req.headers.authorization !== "Bearer isolated-browser-token") {
      res.writeHead(401);
      res.end();
      return;
    }
    const series = url.pathname.includes("/tv");
    const query = url.searchParams.get("query");
    const correct = query === "Correct" || /\/30(?:\/|$)/.test(url.pathname);
    const id = correct ? 30 : series ? 20 : 10;
    const title = correct ? "正确作品" : series ? "暗黑" : "降临";
    const original = correct ? "Correct" : series ? "Dark" : "Arrival";
    const date = correct ? "2020-01-01" : series ? "2017-01-01" : "2016-01-01";
    if (url.pathname.endsWith("/images")) {
      res.setHeader("Content-Type", "application/json");
      res.end(
        JSON.stringify({
          id,
          backdrops:
            mode === "artwork-empty"
              ? []
              : [
                  {
                    file_path: "/thumb.png",
                    iso_639_1: "zh",
                    width: 640,
                    height: 360,
                    vote_average: 8,
                  },
                  {
                    file_path: "/backdrop.png",
                    iso_639_1: null,
                    width: 640,
                    height: 360,
                    vote_average: 9,
                  },
                ],
        }),
      );
      return;
    }
    if (url.pathname.endsWith("credits")) {
      if (mode === "credits-error") {
        res.writeHead(500);
        res.end("Credits unavailable");
        return;
      }
      const cast = Array.from({ length: 12 }, (_, i) => ({
        id: 100 + i,
        name:
          i === 0 ? (correct ? "修正主演" : "测试主演") : `测试演员 ${i + 1}`,
        order: i,
        profile_path: i < 2 ? "/person.png" : null,
        ...(series
          ? { roles: [{ character: "角色甲" }, { character: "角色乙" }] }
          : { character: i === 0 ? "语言学家" : `角色 ${i + 1}` }),
      }));
      const crew = [
        {
          id: 201,
          name: "测试导演",
          profile_path: "/director.png",
          ...(series ? { jobs: [{ job: "Director" }] } : { job: "Director" }),
        },
        {
          id: 202,
          name: "非导演编剧",
          ...(series ? { jobs: [{ job: "Writer" }] } : { job: "Writer" }),
        },
      ];
      res.setHeader("Content-Type", "application/json");
      res.end(
        JSON.stringify({
          id,
          cast: mode === "credits-empty" ? [] : cast,
          crew: mode === "credits-empty" ? [] : crew,
        }),
      );
      return;
    }
    const item = {
      id,
      overview: "中文优先的隔离测试简介，不是真实 TMDB 资料。",
      poster_path: "/poster.png",
      ...(series
        ? { name: title, original_name: original, first_air_date: date }
        : { title, original_title: original, release_date: date }),
    };
    res.setHeader("Content-Type", "application/json");
    res.end(
      JSON.stringify(
        url.pathname.includes("/search/")
          ? {
              results: query === "Nothing" ? [] : [item],
              total_pages: 1,
              total_results: query === "Nothing" ? 0 : 1,
            }
          : item,
      ),
    );
  });
  await new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(18092, "127.0.0.1", resolve);
  });
  return server;
}
