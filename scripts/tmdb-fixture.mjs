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
      res.writeHead(200, { "Content-Type": "image/png" });
      res.end(readFileSync("tests/fixtures/tmdb-poster.png"));
      return;
    }
    if (req.headers.authorization !== "Bearer isolated-browser-token") {
      res.writeHead(401);
      res.end();
      return;
    }
    const series = url.pathname.includes("/tv");
    const query = url.searchParams.get("query");
    const correct = query === "Correct" || url.pathname.endsWith("/30");
    const id = correct ? 30 : series ? 20 : 10;
    const title = correct ? "正确作品" : series ? "暗黑" : "降临";
    const original = correct ? "Correct" : series ? "Dark" : "Arrival";
    const date = correct ? "2020-01-01" : series ? "2017-01-01" : "2016-01-01";
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
