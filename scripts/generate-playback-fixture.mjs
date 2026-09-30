// Generate our own silent, seekable VP8/WebM test video. No downloaded media,
// FFmpeg, runtime transcoding, or third-party fixture licensing is involved.
import { chromium } from "@playwright/test";
import { existsSync, writeFileSync } from "node:fs";
import { createServer } from "node:http";
const server = createServer((_, response) =>
  response.end("<!doctype html><title>fixture</title>"),
);
await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
const chrome = "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome";
const browser = await chromium.launch({
  executablePath:
    process.env.CHROME_BIN || (existsSync(chrome) ? chrome : undefined),
});
try {
  const page = await browser.newPage();
  await page.goto(`http://127.0.0.1:${server.address().port}`);
  const chunks = await page.evaluate(async () => {
    const chunks = [];
    let failure;
    const encoder = new VideoEncoder({
      output: (chunk) => {
        const data = new Uint8Array(chunk.byteLength);
        chunk.copyTo(data);
        chunks.push({
          timestamp: chunk.timestamp,
          key: chunk.type === "key",
          data: Array.from(data),
        });
      },
      error: (error) => {
        failure = error.message;
      },
    });
    encoder.configure({
      codec: "vp8",
      width: 640,
      height: 360,
      bitrate: 400000,
      framerate: 12,
    });
    const canvas = new OffscreenCanvas(640, 360),
      context = canvas.getContext("2d");
    for (let frame = 0; frame < 720; frame++) {
      context.fillStyle = `hsl(${frame % 360},35%,18%)`;
      context.fillRect(0, 0, 640, 360);
      for (let j = 0; j < 70; j++) {
        context.fillStyle = `hsl(${(j * 17 + frame) % 360},50%,${30 + (j % 30)}%)`;
        context.fillRect((j * 137 + frame * 3) % 640, (j * 43) % 360, 36, 24);
      }
      context.fillStyle = "white";
      context.font = "32px sans-serif";
      context.fillText(`REEL NEST  /  ${Math.floor(frame / 12)}s`, 40, 180);
      const image = new VideoFrame(canvas, {
        timestamp: Math.round((frame * 1000000) / 12),
        duration: Math.round(1000000 / 12),
      });
      encoder.encode(image, { keyFrame: frame % 12 === 0 });
      image.close();
      if (frame % 60 === 59) await encoder.flush();
    }
    await encoder.flush();
    encoder.close();
    if (failure) throw new Error(failure);
    return chunks;
  });
  const id = (n) =>
    Buffer.from(
      n
        .toString(16)
        .padStart(n.toString(16).length + (n.toString(16).length % 2), "0"),
      "hex",
    );
  const uint = (n) => {
    const b = Buffer.alloc(8);
    b.writeBigUInt64BE(BigInt(n));
    return b;
  };
  const size = (n) => {
    for (let len = 1; len <= 8; len++)
      if (n < 2 ** (len * 7) - 1) {
        const b = uint(n + 2 ** (len * 7));
        return b.subarray(8 - len);
      }
    throw new Error("oversize");
  };
  const el = (n, payload) => {
    const b = Array.isArray(payload) ? Buffer.concat(payload) : payload;
    return Buffer.concat([id(n), size(b.length), b]);
  };
  const number = (n, v) => el(n, uint(v));
  const string = (n, v) => el(n, Buffer.from(v));
  const duration = Buffer.alloc(8);
  duration.writeDoubleBE(60000);
  const header = el(0x1a45dfa3, [
    number(0x4286, 1),
    number(0x42f7, 1),
    number(0x42f2, 4),
    number(0x42f3, 8),
    string(0x4282, "webm"),
    number(0x4287, 4),
    number(0x4285, 2),
  ]);
  const info = el(0x1549a966, [
    number(0x2ad7b1, 1000000),
    el(0x4489, duration),
    string(0x4d80, "reelnest fixture"),
    string(0x5741, "WebCodecs VP8"),
  ]);
  const tracks = el(0x1654ae6b, [
    el(0xae, [
      number(0xd7, 1),
      number(0x73c5, 1),
      number(0x83, 1),
      string(0x86, "V_VP8"),
      number(0x23e383, 83333333),
      el(0xe0, [number(0xb0, 640), number(0xba, 360)]),
    ]),
  ]);
  const clusters = [];
  for (let second = 0; second < 60; second++) {
    const blocks = chunks.slice(second * 12, second * 12 + 12).map((chunk) => {
      const h = Buffer.alloc(4);
      h[0] = 0x81;
      h.writeInt16BE(Math.round(chunk.timestamp / 1000) - second * 1000, 1);
      h[3] = chunk.key ? 0x80 : 0;
      return el(0xa3, Buffer.concat([h, Buffer.from(chunk.data)]));
    });
    clusters.push(el(0x1f43b675, [number(0xe7, second * 1000), ...blocks]));
  }
  const seek = (target, position) =>
    el(0x4dbb, [el(0x53ab, id(target)), number(0x53ac, position)]);
  const seekHeadSize = el(0x114d9b74, [
    seek(0x1549a966, 0),
    seek(0x1654ae6b, 0),
    seek(0x1c53bb6b, 0),
  ]).length;
  let position = seekHeadSize + info.length + tracks.length;
  const cues = el(
    0x1c53bb6b,
    clusters.map((cluster, second) => {
      const cue = el(0xbb, [
        number(0xb3, second * 1000),
        el(0xb7, [number(0xf7, 1), number(0xf1, position)]),
      ]);
      position += cluster.length;
      return cue;
    }),
  );
  const seekHead = el(0x114d9b74, [
    seek(0x1549a966, seekHeadSize),
    seek(0x1654ae6b, seekHeadSize + info.length),
    seek(0x1c53bb6b, position),
  ]);
  const file = Buffer.concat([
    header,
    el(0x18538067, [seekHead, info, tracks, ...clusters, cues]),
  ]);
  writeFileSync("tests/fixtures/playback.webm", file);
  console.log(
    `Generated ${chunks.length} frames, ${file.length} bytes, 60 seconds, silent VP8/WebM 640x360@12fps`,
  );
} finally {
  await browser.close();
  server.close();
}
