# Playback fixture

`playback.webm` is an original synthetic Reel Nest animation, generated locally
using Chrome WebCodecs and the repository's small EBML/WebM muxer. No downloaded
media, private media, external film footage, FFmpeg or server-side transcoding.

- 60 seconds, 640 × 360, 12 fps, 720 VP8 frames, **no audio track**.
- Duration, one-second keyframes and seek cues are included for real seeking.
- Source: `scripts/generate-playback-fixture.mjs`.
- Regenerate from the repo root: `node scripts/generate-playback-fixture.mjs`.
  Uses `CHROME_BIN`, installed macOS Chrome, or Playwright Chromium. Exact encoded
  bytes may differ with browser codec versions. Normal tests use the committed
  binary and do not regenerate it.
- Browser tests use original file bytes through the running application's HTTP
  boundary, inspect decoded frames and currentTime, and drag the real seek UI.
  Network throttling ensures post-seek byte-range requests reach the real server.
- SRT/VTT fixtures are created in temporary media directories by tests; they are
  never placed beside user media. This silent fixture does not validate audio
  codecs or general container compatibility.

## TMDB fixture poster

`tmdb-poster.png` is a locally generated 120 × 180 RGB gradient PNG, not downloaded
artwork or real TMDB content. The controlled external-service fixture serves it
so browser tests verify actual image decoding through the authenticated cache
endpoint, separately from real TMDB connectivity. Each metadata browser flow
starts a fresh real app process/SQLite database; only the external TMDB is faked.

`tmdb-thumb.png` / `tmdb-backdrop.png` 为本地程序生成的 640×360 几何风景图片，分别用于横版封面及详情背景的真实图片解码/比例测试。不是远程下载的媒体，也不代表真实 TMDB/TVDB 资料。
