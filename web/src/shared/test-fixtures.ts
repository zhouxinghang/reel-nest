import type { Schema } from "./api";
export const card: Schema["MediaCard"] = {
  entryType: "media",
  id: "file1",
  entryKey: "media:file1",
  libraryId: "lib1",
  kind: "movie",
  title: "测试电影",
  filename: "movie.webm",
  state: "available",
  addedAt: 1780000000000,
  favorite: { favorite: false, favoritedAt: null },
  progress: null,
  episode: null,
  metadata: null,
};
export const page: Schema["PageInfo"] = { nextCursor: null, expiresAt: null };
export const status: Schema["MetadataStatus"] = {
  sources: [],
  queued: 0,
  running: 0,
  succeeded: 0,
  failed: 0,
  superseded: 0,
  revision: 1,
  failures: [],
  page,
};
export const provider: Schema["ProviderSettings"] = {
  source: "metatube",
  name: "MetaTube",
  url: "http://nas/v1",
  catalogId: "cat1",
  credentialSet: false,
  pinSet: false,
  configured: true,
  revision: 1,
};
export function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: unknown) => void;
  const promise = new Promise<T>((yes, no) => {
    resolve = yes;
    reject = no;
  });
  return { promise, resolve, reject };
}
