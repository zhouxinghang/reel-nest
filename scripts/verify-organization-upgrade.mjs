// Supply a matching older binary and the current binary. State is created through
// public HTTP, so the migration test never fabricates or queries private SQL.
import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { once } from "node:events";
import { createServer } from "node:net";
import {
  mkdtempSync,
  existsSync,
  readFileSync,
  mkdirSync,
  writeFileSync,
  renameSync,
  rmSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { randomBytes } from "node:crypto";

if (
  ![4, 5].includes(process.argv.length) ||
  (process.argv[4] &&
    !["--playback", "--metadata", "--deletion"].includes(process.argv[4]))
)
  throw new Error(
    "Usage: node scripts/verify-organization-upgrade.mjs /absolute/old-binary /absolute/current-binary [--playback|--metadata|--deletion]",
  );
const [oldBinary, currentBinary] = process.argv
  .slice(2, 4)
  .map((path) => resolve(path));
const metadata = process.argv[4] === "--metadata";
const deletion = process.argv[4] === "--deletion";
const stateful = metadata || deletion;
const playback = stateful || process.argv[4] === "--playback";
let streamPath = "";
const root = mkdtempSync(join(tmpdir(), "reelnest-upgrade-"));
const media = join(root, "media");
const data = join(root, "data");
const password = randomBytes(20).toString("hex");
let child;
let cookie = "";
let baseURL = "";

async function stop() {
  if (!child) return;
  const processToStop = child;
  child = undefined;
  if (
    !processToStop.pid ||
    processToStop.exitCode !== null ||
    processToStop.signalCode !== null
  )
    return;
  const ended = once(processToStop, "exit");
  processToStop.kill("SIGTERM");
  const timeout = setTimeout(() => processToStop.kill("SIGKILL"), 10000);
  try {
    await ended;
  } finally {
    clearTimeout(timeout);
  }
}

async function start(binary, bootstrap) {
  const probe = createServer();
  probe.listen(0, "127.0.0.1");
  await once(probe, "listening");
  const port = probe.address().port;
  await new Promise((resolve, reject) =>
    probe.close((error) => (error ? reject(error) : resolve())),
  );
  baseURL = `http://127.0.0.1:${port}`;
  cookie = "";
  child = spawn(binary, [], {
    env: {
      ...process.env,
      DATA_DIR: data,
      MEDIA_ROOTS: media,
      LISTEN_ADDR: `127.0.0.1:${port}`,
      HOUSEHOLD_USERNAME: bootstrap ? "family" : "",
      HOUSEHOLD_PASSWORD: bootstrap ? password : "",
      HOUSEHOLD_PASSWORD_FILE: "",
      COOKIE_SECURE: "false",
      TMDB_READ_ACCESS_TOKEN: "",
      TMDB_READ_ACCESS_TOKEN_FILE: "",
    },
    stdio: ["ignore", "ignore", "inherit"],
  });
  let startupError;
  child.on("error", (error) => {
    startupError = error;
  });
  const deadline = Date.now() + 15000;
  while (Date.now() < deadline) {
    if (startupError) throw startupError;
    if (child.exitCode !== null)
      throw new Error("Application exited during startup");
    try {
      if ((await fetch(baseURL + "/healthz")).ok) return;
    } catch {}
    await new Promise((resolve) => setTimeout(resolve, 25));
  }
  throw new Error("Application startup timed out");
}

async function request(path, body, status = 200) {
  const response = await fetch(baseURL + path, {
    method: body === undefined ? "GET" : "POST",
    headers: {
      Cookie: cookie,
      ...(body === undefined ? {} : { "Content-Type": "application/json" }),
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  assert.equal(response.status, status, `${path}: unexpected status`);
  const session = response.headers.get("set-cookie");
  if (session) cookie = session.split(";")[0];
  return response.json();
}

try {
  for (const path of [
    "归途/Season 1/S01E01.mp4",
    "flat/归途.S2E2.mp4",
    "归途.S01E01-E02.mp4",
  ]) {
    const target = join(media, path);
    mkdirSync(dirname(target), { recursive: true });
    writeFileSync(target, "isolated pre-upgrade fixture");
  }
  await start(oldBinary, true);
  await request("/api/login", { username: "family", password });
  const directory = await request(
    "/api/directories",
    { path: media, kind: "series" },
    201,
  );
  const deadline = Date.now() + 10000;
  let complete = false;
  while (Date.now() < deadline) {
    const result = await request("/api/directories");
    const state = result.directories.find((item) => item.id === directory.id);
    if (state.scanStatus === "completed") {
      complete = true;
      break;
    }
    assert.equal(state.scanStatus, "running");
    await new Promise((resolve) => setTimeout(resolve, 20));
  }
  assert.ok(complete, "initial scan timed out");
  const before = await request("/api/library");
  assert.equal(before.items.length, 3);
  assert.ok(
    playback
      ? before.series?.length === 1
      : !before.series || before.series.length === 0,
    "old binary must match the requested pre-03/pre-04 baseline",
  );
  if (stateful) {
    if (metadata)
      assert.equal(
        before.items[0].match,
        undefined,
        "old binary must predate TMDB support",
      );
    if (deletion) {
      assert.ok(
        before.items[0].match,
        "old binary must include metadata support",
      );
      await request(
        `/api/media/${before.items[0].id}/deletion-preview`,
        {},
        404,
      );
    }
    const id = before.items[0].id;
    await request(`/api/media/${id}/progress`, {
      position: 12.5,
      duration: 60,
      completed: false,
    });
    streamPath = (await request(`/api/media/${id}/stream-link`, {})).path;
  }
  await stop();
  renameSync(media, media + "-offline");
  await start(currentBinary, false);
  await request("/api/login", { username: "family", password });
  let after = await request("/api/library");
  assert.deepEqual(
    after.items.map((file) => file.id),
    before.items.map((file) => file.id),
  );
  assert.equal(after.series.length, 1);
  assert.equal(after.series[0].title, "归途");
  assert.equal(after.series[0].seasonCount, 2);
  assert.equal(after.series[0].episodeCount, 2);
  assert.equal(after.items.filter((file) => file.episode === null).length, 1);
  const detail = await request(`/api/series/${after.series[0].id}`);
  assert.deepEqual(
    detail.seasons.map((season) => season.number),
    [1, 2],
  );
  if (playback) {
    const id = after.items[0].id;
    if (stateful) {
      assert.equal((await request(`/api/media/${id}/progress`)).position, 12.5);
      assert.equal((await request("/api/metadata/status")).configured, false);
      assert.equal(after.items[0].match.metadata, null);
    } else
      assert.equal((await request(`/api/media/${id}/progress`)).updatedAt, 0);
    renameSync(media + "-offline", media);
    await request(`/api/media/${id}/progress`, {
      position: 12.5,
      duration: 60,
      completed: false,
    });
    const newStreamPath = (await request(`/api/media/${id}/stream-link`, {}))
      .path;
    if (stateful)
      assert.equal(
        newStreamPath,
        streamPath,
        "migration changed stream credential",
      );
    streamPath = newStreamPath;
    const stream = await fetch(baseURL + streamPath, {
      headers: { Range: "bytes=0-7" },
    });
    assert.equal(stream.status, 206);
    assert.equal(await stream.text(), "isolated");
    after = await request("/api/library");
  }
  await stop();
  await start(currentBinary, false);
  await request("/api/login", { username: "family", password });
  assert.deepEqual(await request("/api/library"), after);
  if (playback) {
    const id = after.items[0].id;
    assert.equal((await request(`/api/media/${id}/progress`)).position, 12.5);
    assert.equal(
      (await request(`/api/media/${id}/stream-link`, {})).path,
      streamPath,
    );
    const stream = await fetch(baseURL + streamPath);
    assert.equal(stream.status, 200);
    assert.equal(await stream.text(), "isolated pre-upgrade fixture");
  } else {
    assert.deepEqual(
      await request(`/api/series/${after.series[0].id}`),
      detail,
    );
  }
  if (deletion) {
    const id = after.items[0].id;
    const preview = await request(`/api/media/${id}/deletion-preview`, {});
    assert.equal(preview.path, after.items[0].path);
    const result = await request(`/api/media/${id}/delete`, {
      confirmation: preview.confirmation,
      confirmed: true,
    });
    assert.equal(result.deleted, true);
    assert.equal(result.indexUpdated, true);
    assert.equal(existsSync(preview.path), false);
    for (const file of after.items.slice(1))
      assert.equal(
        readFileSync(file.path, "utf8"),
        "isolated pre-upgrade fixture",
      );
    assert.equal((await fetch(baseURL + streamPath)).status, 410);
    after = await request("/api/library");
    assert.equal(after.items.find((file) => file.id === id).available, false);
    await stop();
    await start(currentBinary, false);
    await request("/api/login", { username: "family", password });
    assert.deepEqual(await request("/api/library"), after);
    assert.equal((await request(`/api/media/${id}/progress`)).position, 12.5);
    assert.equal((await fetch(baseURL + streamPath)).status, 410);
  }
  console.log(
    deletion
      ? "PASS: real metadata version → offline-source upgrade → preserved account, IDs, hierarchy, progress and stream → single-file permanent deletion → restart with correct unavailable state."
      : metadata
        ? "PASS: real pre-TMDB playback application → offline-source metadata migration → restored source → restart; account, IDs, hierarchy, saved progress and original stream credential preserved."
        : playback
          ? "PASS: real pre-04 application → offline-source upgrade → shared progress and independent stream → repeated restart; account, IDs, organization, progress and stream key preserved."
          : "PASS: real pre-03 application → offline-source upgrade → repeated restart; account, file IDs and series organization preserved.",
  );
} finally {
  await stop();
  rmSync(root, { recursive: true, force: true });
}
