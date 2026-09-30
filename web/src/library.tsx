import { useEffect, useState, type FormEvent } from "react";
import { api, message } from "./api";

import type { MediaFile, SeriesSummary } from "./media";
import { SeriesCard, seriesURL } from "./series";
import { Player, clock } from "./player";
import { ExternalPlayer } from "./external-player";

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

export function Library() {
  const [items, setItems] = useState<MediaFile[]>([]);
  const [series, setSeries] = useState<SeriesSummary[]>([]);
  const [error, setError] = useState("");
  const [loaded, setLoaded] = useState(false);
  const [query, setQuery] = useState("");
  const [filter, setFilter] = useState("all");
  useEffect(() => {
    api<{ items: MediaFile[]; series: SeriesSummary[] }>("/api/library")
      .then((result) => {
        setItems(result.items);
        setSeries(result.series);
        setLoaded(true);
      })
      .catch((error) => setError(message(error)));
  }, []);
  const shown = items.filter(
    (item) =>
      (item.filename.toLocaleLowerCase().includes(query.toLocaleLowerCase()) ||
        item.episode?.seriesTitle
          .toLocaleLowerCase()
          .includes(query.toLocaleLowerCase())) &&
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
  return (
    <>
      <div className="page-heading">
        <div>
          <p className="eyebrow">OUR COLLECTION</p>
          <h1>媒体库</h1>
          <p className="muted">
            喜欢的故事，都在这里。
            <span className="count">{items.length} 个媒体文件</span>
          </p>
        </div>
        <a className="secondary" href="/directories">
          ＋ 接入媒体目录
        </a>
      </div>
      {items.some(
        (item) =>
          item.available &&
          item.progress &&
          item.progress.position > 0 &&
          !item.progress.completed,
      ) && (
        <section className="continue-watching" aria-label="继续观看">
          <h2>继续观看</h2>
          <div className="continue-list">
            {items
              .filter(
                (item) =>
                  item.available &&
                  item.progress &&
                  item.progress.position > 0 &&
                  !item.progress.completed,
              )
              .sort((a, b) => b.progress!.updatedAt - a.progress!.updatedAt)
              .map((item) => (
                <a
                  className="secondary"
                  key={item.id}
                  href={`/media/${item.id}?play=1`}
                >
                  {item.filename}
                  <span className="fine">
                    {" "}
                    · {clock(item.progress!.position)} /{" "}
                    {clock(item.progress!.duration)}
                  </span>
                </a>
              ))}
          </div>
        </section>
      )}
      <div className="library-toolbar">
        <div className="filters" role="group" aria-label="媒体分类">
          {[
            ["all", "全部"],
            ["movie", "电影"],
            ["series", "电视剧"],
            ["unmatched", "未匹配媒体"],
          ].map(([value, label]) => (
            <button
              key={value}
              aria-pressed={value === filter}
              className={value === filter ? "selected" : ""}
              onClick={() => setFilter(value)}
            >
              {label}
            </button>
          ))}
        </div>
        <input
          aria-label="搜索文件名"
          className="search"
          placeholder="搜索标题或文件名…"
          value={query}
          onChange={(event) => setQuery(event.target.value)}
        />
      </div>
      {error ? (
        <p role="alert" className="error">
          {error}
        </p>
      ) : !loaded ? (
        <p role="status">正在加载媒体库…</p>
      ) : items.length === 0 ? (
        <section className="empty">
          <span className="empty-icon" aria-hidden="true">
            ▥
          </span>
          <h2>给好故事留一个位置</h2>
          <p>媒体库还是空的，接入媒体目录后即可扫描收录。</p>
          <a className="primary" href="/directories">
            接入第一个媒体目录 →
          </a>
          <p className="fine">无需 TMDB 凭据，也可以浏览本地媒体。</p>
        </section>
      ) : shown.length === 0 ? (
        <section className="empty">
          <h2>没有找到对应的媒体</h2>
          <p>换个文件名或分类试试。</p>
        </section>
      ) : (
        <>
          <p className="collection-note">
            {filter === "unmatched"
              ? "尚未关联影片资料的全部文件，已组织的单集仍会保留在这里"
              : "电影按文件浏览，电视剧按季集组织；无法识别的文件仍独立保留"}
          </p>
          <div className="poster-grid">
            {visibleSeries.map((show) => (
              <SeriesCard key={show.id} series={show} />
            ))}
            {standalone.map((item) => (
              <a
                href={`/media/${item.id}`}
                key={item.id}
                className="media-card"
              >
                <div className="poster-placeholder">
                  <span className="poster-top">
                    REEL NEST / {item.kind === "movie" ? "MOVIE" : "SERIES"}
                  </span>
                  <span className="poster-symbol" aria-hidden="true">
                    ▥
                  </span>
                  <span className="poster-title">
                    {item.filename.replace(/\.[^.]+$/, "")}
                  </span>
                  <span className="poster-bottom">本地收藏</span>
                </div>
                <div className="card-heading">
                  <h2>{item.filename}</h2>
                  {!item.available && (
                    <span className="unavailable">不可用</span>
                  )}
                </div>
                <p>
                  {item.kind === "movie"
                    ? "电影"
                    : item.episode
                      ? `第 ${item.episode.seasonNumber} 季 · 第 ${item.episode.episodeNumber} 集`
                      : "未组织的电视剧文件"}{" "}
                  <span>· 未匹配媒体</span>
                </p>
              </a>
            ))}
          </div>
        </>
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
                ▱
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
  const [play, setPlay] = useState(
    new URLSearchParams(window.location.search).get("play") === "1",
  );
  const [file, setFile] = useState<MediaFile | null>(null);
  const [error, setError] = useState("");
  useEffect(() => {
    api<MediaFile>(`/api/media/${encodeURIComponent(id)}`)
      .then(setFile)
      .catch((error) => setError(message(error)));
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
          <div className="poster-placeholder detail-poster">
            <span className="poster-top">REEL NEST / LOCAL COLLECTION</span>
            <span className="poster-symbol" aria-hidden="true">
              ▥
            </span>
            <span className="poster-title">
              {file.filename.replace(/\.[^.]+$/, "")}
            </span>
            <span className="poster-bottom">本地收藏</span>
          </div>
          <section className="detail-info">
            <p className="eyebrow">
              {file.kind === "movie" ? "MOVIE" : "SERIES FILE"}
            </p>
            <h1>{file.filename}</h1>
            <span className="pill">未匹配媒体</span>
            <p className="detail-description">
              尚未关联影片资料，保留原始文件名供浏览。未匹配媒体也可以直接尝试播放，不要求
              TMDB 凭据。
            </p>
            {file.organizationReason && (
              <p className="notice">识别说明：{file.organizationReason}</p>
            )}
            <div className="media-actions">
              <button className="primary" onClick={() => setPlay(true)}>
                网页播放
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
                {file.available
                  ? "上次完整扫描时可用"
                  : "文件不可用：完整扫描未发现此版本"}
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
              文件状态来自最近一次成功扫描；原文件变更后请重新扫描。
              <br />
              浏览器兼容性取决于容器与编码；无法直放时可使用外部播放器。
            </p>
          </section>
        </div>
      )}
      {file && play && <Player key={file.id} file={file} />}
      {file && <ExternalPlayer key={`external-${file.id}`} id={file.id} />}
    </>
  );
}
