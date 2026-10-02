import { createServer } from "node:http";
import { readFileSync } from "node:fs";

// Contract fixture only: no real MetaTube service or third-party image requests.
export async function startMetaTubeFixture() {
  const server = createServer((req, res) => {
    const url = new URL(req.url, "http://127.0.0.1:18095");
    const parts = url.pathname.split("/").map(decodeURIComponent);
    const json = (data) => {
      res.setHeader("Content-Type", "application/json");
      res.end(JSON.stringify({ data }));
    };
    if (url.pathname.startsWith("/v1/images/")) {
      if (req.headers.authorization || url.search) {
        res.writeHead(400).end();
        return;
      }
      const type = parts[3];
      const name = type === "primary" ? "poster" : type;
      res.setHeader("Content-Type", "image/png");
      res.end(readFileSync(`tests/fixtures/tmdb-${name}.png`));
      return;
    }
    if (
      req.headers.authorization !== "Bearer isolated-browser-metatube-token"
    ) {
      res.writeHead(401).end();
      return;
    }
    const movie = (provider) => ({
      id: "abc123",
      provider,
      number: "ABC-123",
      title: "MetaTube 示例影片",
      summary: "MetaTube 隔离接口测试简介",
      homepage: "https://example.invalid/movie/abc123",
      cover_url: "https://not-fetched.invalid/cover.jpg",
      release_date: "2024-01-01T00:00:00Z",
      actors: ["MetaTube 演员"],
      director: "MetaTube 导演",
    });
    if (url.pathname === "/v1/movies/search") {
      const q = url.searchParams.get("q");
      json(
        q === "选择" ? [movie("Example"), movie("Other")] : [movie("Example")],
      );
    } else if (parts[2] === "movies" && parts[4] === "abc123") {
      json(movie(parts[3]));
    } else {
      res.writeHead(404).end();
    }
  });
  await new Promise((resolve) => server.listen(18095, "127.0.0.1", resolve));
  return server;
}
