import { DeleteSourceFile } from "./deletion";
import { useEffect, useState, type FormEvent } from "react";
import { api, message } from "./api";

import type { MediaFile, SeriesSummary } from "./media";
import { SeriesCard, seriesURL } from "./series";
import { Player, clock } from "./player";
import { ExternalPlayer } from "./external-player";

import { Matching } from "./matching";
import { Poster, MetadataInfo } from "./metadata";
import { Icon } from "./icons";

type Directory = {
  id: string;
  path: string;
  kind: "movie" | "series";
  scanStatus: "pending" | "running" | "completed" | "failed" | "interrupted";
  scanError: string;
  scanStarted: number;
  scanFinished: number;
};
type DirectoriesResponse = { directories: Directory[]; allowedRoots: string[] };
const scanLabels = {
  pending: "等待扫描",
  running: "扫描中",
  completed: "扫描完成",
  failed: "扫描失败",
  interrupted: "扫描已中断",
};

export type LibraryFilter = "all" | "movie" | "series" | "unmatched";

export function readLibraryFilter(): LibraryFilter {
  const value = new URLSearchParams(window.location.search).get("type");
  return value === "movie" || value === "series" || value === "unmatched"
    ? value
    : "all";
}

function MediaCard({ item }: { item: MediaFile }) {
  const title = item.match.metadata?.title || item.filename;
  return (
    <a href={`/media/${item.id}`} className="media-card" title={title}>
      <Poster
        metadata={item.match.metadata}
        title={
          item.match.metadata?.title || item.filename.replace(/\.[^.]+$/, "")
        }
      >
        {item.progress?.completed && (
          <span className="poster-badge watched" role="img" aria-label="已看完">
            <Icon name="check" />
          </span>
        )}
        <span className="poster-hover" aria-hidden="true">
          <Icon name="arrow" />
          <span>查看详情</span>
        </span>
      </Poster>
      <div className="card-heading">
        <h2>{title}</h2>
        {!item.available && <span className="unavailable">不可用</span>}
      </div>
      <p>
        {item.kind === "movie"
          ? "电影"
          : item.episode
            ? `第 ${item.episode.seasonNumber} 季 · 第 ${item.episode.episodeNumber} 集`
            : "未组织的电视剧文件"}
        <span>
          {" "}
          · {item.matched ? item.match.metadata?.year || "TMDB" : "未匹配媒体"}
        </span>
      </p>
    </a>
  );
}

export function Library({
  filter,
  onFilterChange,
}: {
  filter: LibraryFilter;
  onFilterChange: (value: LibraryFilter) => void;
}) {
  const [items, setItems] = useState<MediaFile[]>([]);
  const [series, setSeries] = useState<SeriesSummary[]>([]);
  const [error, setError] = useState("");
  const [loaded, setLoaded] = useState(false);
  const [query, setQuery] = useState("");
  const [view, setView] = useState<"poster" | "list">("poster");
  const [sort, setSort] = useState("recent");
  useEffect(() => {
    let active = true;
    const refresh = () => {
      void api<{ items: MediaFile[]; series: SeriesSummary[] }>("/api/library")
        .then((result) => {
          if (!active) return;
          setItems(result.items);
          setSeries(result.series);
          setError("");
          setLoaded(true);
        })
        .catch((error) => {
          if (active) setError(message(error));
        });
    };
    refresh();
    window.addEventListener("metadata-updated", refresh);
    return () => {
      active = false;
      window.removeEventListener("metadata-updated", refresh);
    };
  }, []);
  const search = query.trim().toLocaleLowerCase();
  const seriesTitles = new Map(
    series.map((show) => [show.id, show.match.metadata?.title || show.title]),
  );
  const shown = items.filter(
    (item) =>
      [
        item.match.metadata?.title,
        item.filename,
        item.episode?.seriesTitle,
        item.episode && seriesTitles.get(item.episode.seriesId),
      ].some((title) => title?.toLocaleLowerCase().includes(search)) &&
      (filter === "all" ||
        filter === item.kind ||
        (filter === "unmatched" && !item.matched)),
  );
  const visibleSeriesIDs = new Set(shown.map((item) => item.episode?.seriesId));
  const visibleSeries =
    filter === "unmatched"
      ? []
      : series.filter((item) => visibleSeriesIDs.has(item.id));
  const groupedIDs = new Set(visibleSeries.map((item) => item.id));
  const standalone = shown.filter(
    (item) => !item.episode || !groupedIDs.has(item.episode.seriesId),
  );
  const seriesModified = new Map<string, number>();
  for (const item of items) {
    if (item.episode)
      seriesModified.set(
        item.episode.seriesId,
        Math.max(
          seriesModified.get(item.episode.seriesId) || 0,
          item.modifiedAt,
        ),
      );
  }
  const entries = [
    ...visibleSeries.map((show) => ({
      id: show.id,
      title: show.match.metadata?.title || show.title,
      modifiedAt: seriesModified.get(show.id) || 0,
      content: <SeriesCard key={show.id} series={show} />,
    })),
    ...standalone.map((item) => ({
      id: item.id,
      title: item.match.metadata?.title || item.filename,
      modifiedAt: item.modifiedAt,
      content: <MediaCard key={item.id} item={item} />,
    })),
  ].sort(
    (a, b) =>
      (sort === "recent" ? b.modifiedAt - a.modifiedAt : 0) ||
      a.title.localeCompare(b.title, "zh-CN", { numeric: true }) ||
      a.id.localeCompare(b.id),
  );
  const continuing = items
    .filter(
      (item) =>
        item.available &&
        item.progress &&
        item.progress.position > 0 &&
        !item.progress.completed,
    )
    .sort((a, b) => b.progress!.updatedAt - a.progress!.updatedAt);
  const filterLabels = {
    all: "全部收藏",
    movie: "电影",
    series: "电视剧",
    unmatched: "未匹配媒体",
  };
  const chooseFilter = (value: LibraryFilter) => {
    onFilterChange(value);
    setQuery("");
  };
  return (
    <>
      <div className="page-heading">
        <div>
          <p className="eyebrow">YOUR HOME CINEMA</p>
          <h1>媒体库</h1>
          <p className="muted">
            好故事，随时开场。
            <span className="count">
              {loaded ? `${items.length} 个媒体文件` : "正在读取收藏"}
            </span>
          </p>
        </div>
        <a className="secondary" href="/directories">
          <Icon name="plus" /> 接入媒体目录
        </a>
      </div>
      {loaded && items.length > 0 && (
        <section className="library-hubs" aria-label="我的媒体">
          {(
            [
              [
                "movie",
                "电影",
                "film",
                `${items.filter((item) => item.kind === "movie").length} 个媒体文件`,
              ],
              [
                "series",
                "电视剧",
                "tv",
                `${series.length} 部电视剧 · ${items.filter((item) => item.kind === "series").length} 个文件`,
              ],
              [
                "unmatched",
                "未匹配媒体",
                "unmatched",
                `${items.filter((item) => !item.matched).length} 个待完善资料的文件`,
              ],
            ] as const
          ).map(([value, label, icon, count]) => (
            <button
              key={value}
              className={`library-hub hub-${value}`}
              aria-label={`浏览${label}`}
              aria-pressed={filter === value}
              onClick={() => chooseFilter(value)}
            >
              <span className="hub-icon">
                <Icon name={icon} />
              </span>
              <span>
                <strong>{label}</strong>
                <small>{count}</small>
              </span>
              <Icon name="chevron" className="hub-arrow" />
            </button>
          ))}
        </section>
      )}
      {continuing.length > 0 && filter === "all" && !search && (
        <section className="continue-watching" aria-label="继续观看">
          <div className="section-heading">
            <h2>继续观看</h2>
            <span className="fine">
              <Icon name="clock" /> 家庭共享进度
            </span>
          </div>
          <div className="continue-list">
            {continuing.map((item) => (
              <a
                className="continue-card"
                key={item.id}
                href={`/media/${item.id}?play=1`}
                title={item.filename}
              >
                <div className="continue-art">
                  <Poster
                    metadata={item.match.metadata}
                    title={
                      item.match.metadata?.title ||
                      item.filename.replace(/\.[^.]+$/, "")
                    }
                  />
                  <span className="continue-play" aria-hidden="true">
                    <Icon name="play" />
                  </span>
                  <span className="continue-time">
                    {clock(item.progress!.position)} /{" "}
                    {clock(item.progress!.duration)}
                  </span>
                  <progress
                    className="watch-progress"
                    aria-label={`${item.filename} 观看进度`}
                    max={Math.max(1, item.progress!.duration)}
                    value={item.progress!.position}
                  />
                </div>
                <strong>{item.match.metadata?.title || item.filename}</strong>
                <span className="fine">
                  {item.episode
                    ? `第 ${item.episode.seasonNumber} 季 · 第 ${item.episode.episodeNumber} 集`
                    : "电影"}{" "}
                  · 继续上次的故事
                </span>
              </a>
            ))}
          </div>
        </section>
      )}
      <div className="library-toolbar">
        <div className="filters" role="group" aria-label="媒体分类">
          {(
            [
              ["all", "全部"],
              ["movie", "电影"],
              ["series", "电视剧"],
              ["unmatched", "未匹配媒体"],
            ] as const
          ).map(([value, label]) => (
            <button
              key={value}
              aria-pressed={value === filter}
              className={value === filter ? "selected" : ""}
              onClick={() => onFilterChange(value)}
            >
              {label}
            </button>
          ))}
        </div>
        <div className="search-field">
          <Icon name="search" />
          <input
            id="library-search"
            aria-label="搜索文件名"
            type="search"
            placeholder="搜索标题或文件名"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            autoFocus={
              new URLSearchParams(window.location.search).get("focus") ===
              "search"
            }
          />
        </div>
      </div>
      {error ? (
        <p role="alert" className="error">
          {error}
        </p>
      ) : !loaded ? (
        <div className="library-loading" role="status">
          <span className="loading-spinner" />
          正在加载媒体库…
        </div>
      ) : items.length === 0 ? (
        <section className="empty">
          <span className="empty-icon">
            <Icon name="film" />
          </span>
          <p className="eyebrow">A COLLECTION OF YOUR OWN</p>
          <h2>给好故事留一个位置</h2>
          <p>媒体库还是空的，接入媒体目录后即可扫描收录。</p>
          <a className="primary" href="/directories">
            <Icon name="plus" /> 接入第一个媒体目录
          </a>
          <p className="fine">无需 TMDB 凭据，也可以浏览本地媒体。</p>
        </section>
      ) : shown.length === 0 ? (
        <section className="empty search-empty">
          <span className="empty-icon">
            <Icon name="search" />
          </span>
          <h2>没有找到对应的媒体</h2>
          <p>换个文件名或分类试试。</p>
          <button className="secondary" onClick={() => chooseFilter("all")}>
            查看全部媒体
          </button>
        </section>
      ) : (
        <section aria-label="媒体收藏">
          <div className="section-heading collection-heading">
            <div>
              <h2>{filterLabels[filter]}</h2>
              <span className="count" aria-live="polite">
                {entries.length} {filter === "unmatched" ? "个文件" : "项收藏"}
              </span>
            </div>
            <div className="collection-controls">
              <select
                aria-label="排序方式"
                value={sort}
                onChange={(event) => setSort(event.target.value)}
              >
                <option value="recent">最近更新</option>
                <option value="title">标题排序</option>
              </select>
              <div className="view-switch" role="group" aria-label="显示方式">
                <button
                  className="icon-button"
                  aria-label="海报视图"
                  title="海报视图"
                  aria-pressed={view === "poster"}
                  onClick={() => setView("poster")}
                >
                  <Icon name="library" />
                </button>
                <button
                  className="icon-button"
                  aria-label="列表视图"
                  title="列表视图"
                  aria-pressed={view === "list"}
                  onClick={() => setView("list")}
                >
                  <Icon name="list" />
                </button>
              </div>
            </div>
          </div>
          {filter === "unmatched" && (
            <p className="collection-note">
              尚未关联影片资料的全部文件，已组织的单集仍会保留在这里。
            </p>
          )}
          <div className={`poster-grid${view === "list" ? " is-list" : ""}`}>
            {entries.map((entry) => entry.content)}
          </div>
        </section>
      )}
    </>
  );
}

export function Directories() {
  const [data, setData] = useState<DirectoriesResponse | null>(null);
  const [path, setPath] = useState("");
  const [kind, setKind] = useState("movie");
  const [error, setError] = useState("");
  const [loadError, setLoadError] = useState("");
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    let active = true;
    let timer: ReturnType<typeof setTimeout> | undefined;
    async function refresh() {
      try {
        const result = await api<DirectoriesResponse>("/api/directories");
        if (active) {
          setData(result);
          setLoadError("");
          setPath((current) => current || result.allowedRoots[0] || "");
        }
      } catch (error) {
        if (active) setLoadError(message(error));
      }
      if (active) timer = setTimeout(refresh, 1000);
    }
    void refresh();
    return () => {
      active = false;
      clearTimeout(timer);
    };
  }, []);
  async function add(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy(true);
    setError("");
    try {
      await api("/api/directories", { path, kind });
      setData(await api("/api/directories"));
    } catch (error) {
      setError(message(error));
    } finally {
      setBusy(false);
    }
  }
  async function scan(id: string) {
    setError("");
    try {
      await api(`/api/directories/${id}/scan`, {});
      setData(await api("/api/directories"));
    } catch (error) {
      setError(message(error));
    }
  }
  return (
    <>
      <div className="page-heading">
        <div>
          <p className="eyebrow">MAKE ROOM FOR STORIES</p>
          <h1>媒体目录</h1>
          <p className="muted">连接已经存放在本机或 NAS 的珍藏。</p>
        </div>
        <a className="secondary" href="/library">
          返回媒体库 →
        </a>
      </div>
      <section className="panel">
        <h2>接入一个媒体目录</h2>
        <p className="muted small">
          填写应用可见的绝对路径。NAS 请先在宿主机挂载；容器内使用挂载后的路径。
        </p>
        {loadError && (
          <p role="alert" className="error">
            {loadError}
          </p>
        )}
        {data && data.allowedRoots.length === 0 ? (
          <p className="notice">
            尚未配置允许接入的根目录。请由部署者设置 MEDIA_ROOTS
            并重启应用，再添加媒体目录。
          </p>
        ) : (
          <>
            <p className="fine">
              允许接入的根目录：
              {data?.allowedRoots.map((root) => (
                <code className="code-path" key={root}>
                  {root}
                </code>
              ))}
            </p>
            <form className="directory-form" onSubmit={add}>
              <div>
                <label htmlFor="media-path">媒体目录路径</label>
                <input
                  id="media-path"
                  required
                  value={path}
                  onChange={(event) => setPath(event.target.value)}
                  placeholder="/media/movies"
                />
              </div>
              <div>
                <label htmlFor="media-kind">内容类型</label>
                <select
                  id="media-kind"
                  value={kind}
                  onChange={(event) => setKind(event.target.value)}
                >
                  <option value="movie">电影</option>
                  <option value="series">电视剧</option>
                </select>
              </div>
              <button className="primary" disabled={busy || !data}>
                {busy ? "正在接入…" : "添加并扫描"}
              </button>
            </form>
          </>
        )}
        {error && (
          <p role="alert" className="error">
            {error}
          </p>
        )}
        <p className="fine directory-tip">
          新增后自动扫描一次，之后由你手动重新扫描。不会上传、改名或移动源文件。
        </p>
      </section>
      <section className="directory-list">
        <h2>
          已接入的目录{" "}
          <span className="count">{data?.directories.length || 0}</span>
        </h2>
        {data?.directories.length === 0 && (
          <p className="muted">还没有接入媒体目录。</p>
        )}
        {data?.directories.map((dir) => (
          <article className="directory-row" key={dir.id}>
            <div className="directory-row-main">
              <span className="directory-icon" aria-hidden="true">
                <Icon name="folder" />
              </span>
              <div>
                <h3>{dir.path}</h3>
                <p className="fine">
                  {dir.kind === "movie" ? "电影" : "电视剧"} ·{" "}
                  <span
                    role="status"
                    className={`scan-status ${dir.scanStatus}`}
                  >
                    {scanLabels[dir.scanStatus]}
                  </span>
                  {dir.scanFinished > 0 &&
                    ` · ${new Date(dir.scanFinished * 1000).toLocaleString()}`}
                </p>
              </div>
              <button
                className="secondary"
                disabled={
                  dir.scanStatus === "running" || dir.scanStatus === "pending"
                }
                onClick={() => scan(dir.id)}
              >
                重新扫描
              </button>
            </div>
            {dir.scanError && (
              <p role="alert" className="error">
                {dir.scanError}
              </p>
            )}
          </article>
        ))}
      </section>
    </>
  );
}

export function MediaDetails({ id }: { id: string }) {
  const [deleted, setDeleted] = useState(false);
  const [play, setPlay] = useState(
    new URLSearchParams(window.location.search).get("play") === "1",
  );
  const [file, setFile] = useState<MediaFile | null>(null);
  const [error, setError] = useState("");
  useEffect(() => {
    const refresh = () => {
      void api<MediaFile>(`/api/media/${encodeURIComponent(id)}`)
        .then(setFile)
        .catch((error) => setError(message(error)));
    };
    refresh();
    window.addEventListener("metadata-updated", refresh);
    return () => window.removeEventListener("metadata-updated", refresh);
  }, [id]);
  return (
    <>
      <nav className="detail-breadcrumbs" aria-label="媒体文件导航">
        <a className="back-link" href="/library">
          ← 返回媒体库
        </a>
        {file?.episode && (
          <>
            <a className="back-link" href={seriesURL(file.episode.seriesId)}>
              {file.episode.seriesTitle}
            </a>
            <a
              className="back-link"
              href={seriesURL(file.episode.seriesId, file.episode.seasonNumber)}
            >
              返回第 {file.episode.seasonNumber} 季
            </a>
          </>
        )}
      </nav>
      {error ? (
        <p role="alert" className="error">
          {error}
        </p>
      ) : !file ? (
        <p role="status">正在加载媒体文件…</p>
      ) : (
        <div className="detail-layout">
          <Poster
            detail
            metadata={file.match.metadata}
            title={
              file.match.metadata?.title ||
              file.filename.replace(/\.[^.]+$/, "")
            }
          />
          <section className="detail-info">
            <p className="eyebrow">
              {file.kind === "movie" ? "MOVIE" : "SERIES FILE"}
            </p>
            <h1>{file.match.metadata?.title || file.filename}</h1>
            {file.matched && (
              <p className="fine file-path">原始文件：{file.filename}</p>
            )}
            <MetadataInfo match={file.match} />
            <Matching
              target={{
                kind: file.kind,
                id: file.episode?.seriesId || file.id,
                title:
                  file.match.metadata?.title ||
                  file.episode?.seriesTitle ||
                  file.filename.replace(/\.[^.]+$/, ""),
              }}
            />
            {file.organizationReason && (
              <p className="notice">识别说明：{file.organizationReason}</p>
            )}
            <div className="media-actions">
              <button
                className="primary"
                disabled={deleted}
                onClick={() => setPlay(true)}
              >
                <Icon name="play" /> 网页播放
              </button>
            </div>
            <dl>
              {file.episode && (
                <>
                  <dt>所属电视剧</dt>
                  <dd>{file.episode.seriesTitle}</dd>
                  <dt>季与单集</dt>
                  <dd>
                    第 {file.episode.seasonNumber} 季 · 第{" "}
                    {file.episode.episodeNumber} 集
                  </dd>
                </>
              )}
              <dt>媒体类型</dt>
              <dd>{file.kind === "movie" ? "电影" : "电视剧文件"}</dd>
              <dt>文件状态</dt>
              <dd>
                {file.available && !deleted
                  ? "上次完整扫描时可用"
                  : "文件不可用：已删除或完整扫描未发现此版本"}
              </dd>
              <dt>文件大小</dt>
              <dd>{file.size.toLocaleString()} 字节</dd>
              <dt>修改时间</dt>
              <dd>{new Date(file.modifiedAt * 1000).toLocaleString()}</dd>
              <dt>源文件路径</dt>
              <dd className="file-path">{file.path}</dd>
              <dt>媒体文件标识</dt>
              <dd className="file-path">{file.id}</dd>
            </dl>
            <p className="fine">
              文件状态来自最近一次成功扫描或删除更新；原文件变更后请重新扫描。
              <br />
              浏览器兼容性取决于容器与编码；无法直放时可使用外部播放器。
            </p>
          </section>
        </div>
      )}
      {file && (
        <DeleteSourceFile
          key={`delete-${file.id}`}
          id={file.id}
          onDeleted={() => {
            setDeleted(true);
            setPlay(false);
            setFile((current) =>
              current ? { ...current, available: false } : current,
            );
          }}
        />
      )}
      {file && play && !deleted && <Player key={file.id} file={file} />}
      {file && !deleted && (
        <ExternalPlayer key={`external-${file.id}`} id={file.id} />
      )}
    </>
  );
}
