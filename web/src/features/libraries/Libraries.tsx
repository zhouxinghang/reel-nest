import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useSearchParams } from "react-router-dom";
import { APIError, type Schema } from "../../shared/api";
import { useAPI } from "../../shared/services";
import {
  Loading,
  QueryError,
  Pagination,
  ui,
  useStaleCursor,
} from "../../shared/ui";
import { librariesQuery } from "./queries";
import { useScans } from "../tasks/Scans";
import { invalidateMetadata } from "../catalog/cache";
import styles from "./libraries.module.css";

export function SourceOrder({
  value,
  onChange,
}: {
  value: Schema["Sources"];
  onChange: (v: Schema["Sources"]) => void;
}) {
  return (
    <fieldset>
      <legend>资料源顺序（从上到下；全不选则关闭自动获取）</legend>
      {[
        ...value,
        ...(["tmdb", "tvdb", "metatube"] as const).filter(
          (s) => !value.includes(s),
        ),
      ].map((source) => (
        <div className={styles.source} key={source}>
          <label>
            <input
              type="checkbox"
              checked={value.includes(source)}
              onChange={(e) =>
                onChange(
                  e.target.checked
                    ? [...value, source]
                    : value.filter((s) => s !== source),
                )
              }
            />
            {source.toUpperCase()}
          </label>
          {value.includes(source) && (
            <button
              type="button"
              className={ui.button}
              disabled={value.indexOf(source) === 0}
              aria-label={`上移 ${source}`}
              onClick={() => {
                const next = [...value];
                const i = next.indexOf(source);
                [next[i - 1], next[i]] = [next[i], next[i - 1]];
                onChange(next);
              }}
            >
              ↑
            </button>
          )}
        </div>
      ))}
    </fieldset>
  );
}
function Editor({
  library,
  onSaved,
}: {
  library: Schema["Library"];
  onSaved: () => void;
}) {
  const api = useAPI();
  const client = useQueryClient();
  const [baseline, setBaseline] = useState(library);
  const [name, setName] = useState(library.name);
  const [sources, setSources] = useState(library.sources);
  const [conflict, setConflict] = useState(false);
  const [reloaded, setReloaded] = useState(false);
  const [reloadError, setReloadError] = useState<unknown>(null);
  const [reloading, setReloading] = useState(false);
  const mutation = useMutation({
    mutationFn: () =>
      api("patch", "/api/libraries/{id}", {
        path: { id: library.id },
        body: { revision: baseline.revision, name, sources },
      }),
    onSuccess: async (data) => {
      setBaseline(data);
      await client.invalidateQueries({ queryKey: ["libraries"] });
      await invalidateMetadata(client);
      onSaved();
    },
    onError: (e) => {
      if (e instanceof APIError && e.code === "REVISION_CONFLICT")
        setConflict(true);
    },
  });
  return (
    <form
      aria-label={`编辑 ${library.name}`}
      onSubmit={(e) => {
        e.preventDefault();
        if (!conflict) mutation.mutate();
      }}
    >
      <label>
        媒体库名称
        <input
          className={ui.input}
          value={name}
          maxLength={80}
          required
          onChange={(e) => setName(e.target.value)}
        />
      </label>
      <SourceOrder value={sources} onChange={setSources} />
      <QueryError error={reloadError || mutation.error} />
      {conflict && (
        <p role="alert">媒体库已被其他会话修改；草稿已保留，请读取最新版本。</p>
      )}
      {reloaded && (
        <p role="status">
          已读取版本 {baseline.revision}，草稿未变；请核对后保存。
        </p>
      )}
      {conflict && (
        <button
          type="button"
          className={ui.button}
          disabled={reloading}
          onClick={async () => {
            setReloading(true);
            setReloadError(null);
            try {
              await client.refetchQueries(
                { queryKey: ["libraries"], type: "active" },
                { throwOnError: true },
              );
              const latest = client
                .getQueriesData<Schema["LibraryPage"]>({
                  queryKey: ["libraries"],
                })
                .flatMap(([, data]) => data?.items || [])
                .find((item) => item.id === library.id);
              if (!latest)
                throw new Error("媒体库已不在当前页，请重新打开管理页");
              setBaseline(latest);
              setConflict(false);
              setReloaded(true);
              mutation.reset();
            } catch (error) {
              setReloadError(error);
            } finally {
              setReloading(false);
            }
          }}
        >
          读取最新版本，保留草稿
        </button>
      )}
      <button className={ui.primary} disabled={conflict || mutation.isPending}>
        保存媒体库
      </button>
    </form>
  );
}
function LibraryRow({ library }: { library: Schema["Library"] }) {
  const api = useAPI();
  const client = useQueryClient();
  const { scans, errors } = useScans();
  const [editing, setEditing] = useState(false);
  const scan = library.latestScanId
    ? scans.get(library.latestScanId)
    : undefined;
  const busy = scan?.state === "running" || scan?.state === "queued";
  const start = useMutation({
    mutationFn: () =>
      api("post", "/api/libraries/{id}/scans", {
        path: { id: library.id },
        body: {},
      }),
    onSuccess: () => client.invalidateQueries({ queryKey: ["libraries"] }),
  });
  const refresh = useMutation({
    mutationFn: () =>
      api("post", "/api/metadata/refresh", { body: { libraryId: library.id } }),
    onSuccess: () => client.invalidateQueries({ queryKey: ["tasks"] }),
  });
  const labels: Record<Schema["Scan"]["state"], string> = {
    queued: "等待扫描",
    running: "正在扫描",
    completed: "扫描完成",
    failed: "扫描失败",
    interrupted: "扫描中断",
  };
  return (
    <section className={styles.panel} aria-label={library.name}>
      <h2>
        <Link to={`/library?library=${encodeURIComponent(library.id)}`}>
          {library.name} →
        </Link>
      </h2>
      <p className={styles.path}>{library.path}</p>
      <p>
        {library.kind === "movie" ? "电影" : "电视剧"} ·{" "}
        {library.sources.join(" → ") || "自动获取已关闭"}
      </p>
      <p role="status">
        {scan
          ? `${labels[scan.state]} · ${scan.discoveredCount} 个文件${scan.errorCode ? ` · ${scan.errorCode}` : ""}`
          : errors.has(library.latestScanId || "")
            ? "扫描状态暂不可用，将稍后重查"
            : "正在读取扫描状态…"}
      </p>
      <div className={styles.actions}>
        <button className={ui.button} onClick={() => setEditing(!editing)}>
          编辑媒体库
        </button>
        <button
          className={ui.button}
          disabled={busy || start.isPending}
          onClick={() => start.mutate()}
        >
          重新扫描
        </button>
        <button
          className={ui.button}
          disabled={refresh.isPending}
          onClick={() => refresh.mutate()}
        >
          刷新影片资料
        </button>
      </div>
      <QueryError error={start.error || refresh.error} />
      {refresh.isSuccess && (
        <p role="status">资料刷新已排队；不会改动源文件。</p>
      )}
      {editing && (
        <Editor library={library} onSaved={() => setEditing(false)} />
      )}
    </section>
  );
}
export function Libraries() {
  const api = useAPI();
  const client = useQueryClient();
  const [search] = useSearchParams();
  const query = useQuery(librariesQuery(api, search.get("cursor") || ""));
  useStaleCursor(query.error);
  const { watch } = useScans();
  useEffect(() => {
    if (query.data)
      watch(
        query.data.items.flatMap((l) =>
          l.latestScanId ? [l.latestScanId] : [],
        ),
      );
  }, [query.data, watch]);
  const [name, setName] = useState("");
  const [path, setPath] = useState("");
  const [kind, setKind] = useState<Schema["Kind"]>("movie");
  const [sources, setSources] = useState<Schema["Sources"]>(["tmdb", "tvdb"]);
  const create = useMutation({
    mutationFn: () =>
      api("post", "/api/libraries", { body: { name, path, kind, sources } }),
    onSuccess: async () => {
      setName("");
      setPath("");
      await client.invalidateQueries({ queryKey: ["libraries"] });
      await invalidateMetadata(client);
    },
  });
  return (
    <>
      <h1>媒体库管理</h1>
      <p className={ui.muted}>
        接入已挂载的媒体目录；不会上传、移动或重命名源文件。新建后自动扫描。
      </p>
      <QueryError error={query.error} retry={() => void query.refetch()} />
      {query.isPending && <Loading />}
      {query.data && (
        <>
          <form
            className={styles.panel}
            aria-label="新建媒体库"
            onSubmit={(e) => {
              e.preventDefault();
              create.mutate();
            }}
          >
            <h2>新建媒体库</h2>
            <p className={styles.path}>
              允许的根目录：
              {query.data.allowedRoots.join("；") || "尚未配置 MEDIA_ROOTS"}
            </p>
            <label>
              媒体库名称
              <input
                className={ui.input}
                value={name}
                required
                maxLength={80}
                onChange={(e) => setName(e.target.value)}
              />
            </label>
            <label>
              媒体目录路径
              <input
                className={ui.input}
                value={path}
                required
                onChange={(e) => setPath(e.target.value)}
              />
            </label>
            <label>
              内容类型
              <select
                className={ui.input}
                value={kind}
                onChange={(e) => setKind(e.target.value as Schema["Kind"])}
              >
                <option value="movie">电影</option>
                <option value="series">电视剧</option>
              </select>
            </label>
            <SourceOrder value={sources} onChange={setSources} />
            <QueryError error={create.error} />
            {create.isSuccess && (
              <p role="status">媒体库已创建，首次扫描已提交。</p>
            )}
            <button
              className={ui.primary}
              disabled={create.isPending || !query.data.allowedRoots.length}
            >
              创建并扫描
            </button>
          </form>
          {query.data.items.map((library) => (
            <LibraryRow key={library.id} library={library} />
          ))}
          <Pagination page={query.data.page} />
        </>
      )}
    </>
  );
}
