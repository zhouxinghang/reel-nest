import { DeleteSourceFile } from "./deletion";
import { useEffect, useState, type FormEvent } from "react";
import { api, message } from "./api";

import type { MediaFile } from "./media";
import { seriesURL } from "./series";
import { Player, clock } from "./player";
import { ExternalPlayer } from "./external-player";

import { Matching } from "./matching";
import { DetailHero, fileSize } from "./detail";
import { Icon } from "./icons";
import { FavoriteButton } from "./favorite";

import {
  SourceOrder,
  DirectorySources,
  type MetadataSource,
  type SourceStatus,
} from "./metadata-sources";

type Directory = {
  metadataSources: MetadataSource[];
  id: string;
  path: string;
  kind: "movie" | "series";
  scanStatus: "pending" | "running" | "completed" | "failed" | "interrupted";
  scanError: string;
  scanStarted: number;
  scanFinished: number;
};
type DirectoriesResponse = {
  directories: Directory[];
  allowedRoots: string[];
  metadataSources: SourceStatus[];
};
const scanLabels = {
  pending: "等待扫描",
  running: "扫描中",
  completed: "扫描完成",
  failed: "扫描失败",
  interrupted: "扫描已中断",
};

export { Library, readLibraryFilter, type LibraryFilter } from "./home";

export function Directories() {
  const [data, setData] = useState<DirectoriesResponse | null>(null);
  const [path, setPath] = useState("");
  const [kind, setKind] = useState("movie");
  const [sources, setSources] = useState<MetadataSource[]>(["tmdb", "tvdb"]);
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
      await api("/api/directories", { path, kind, metadataSources: sources });
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
              <SourceOrder
                value={sources}
                onChange={setSources}
                statuses={data?.metadataSources || []}
                disabled={busy}
              />
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
        <p className="fine">
          凭据由部署者配置：TMDB_READ_ACCESS_TOKEN_FILE、TVDB_API_KEY_FILE，以及需要订阅时的
          TVDB_PIN_FILE；MetaTube 使用 METATUBE_URL 和 METATUBE_TOKEN_FILE（或
          METATUBE_TOKEN）。更改配置后重启应用。
        </p>
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
            <DirectorySources
              key={`${dir.id}:${JSON.stringify(dir.metadataSources)}`}
              id={dir.id}
              sources={dir.metadataSources}
              statuses={data.metadataSources}
              saved={async () => setData(await api("/api/directories"))}
            />
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
  const [fromStart, setFromStart] = useState(false);
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
  const playerVisible = Boolean(file && play && !deleted);
  const progress = file?.progress;
  const resume = progress && !progress.completed && progress.position > 0;
  const format = file?.filename.includes(".")
    ? file.filename.split(".").pop()?.toUpperCase()
    : null;
  return (
    <>
      {file?.episode && (
        <nav className="detail-breadcrumbs" aria-label="媒体文件导航">
          <a className="back-link" href={seriesURL(file.episode.seriesId)}>
            {file.episode.seriesTitle}
          </a>
          <Icon name="chevron" />
          <a
            className="back-link"
            href={seriesURL(file.episode.seriesId, file.episode.seasonNumber)}
          >
            返回第 {file.episode.seasonNumber} 季
          </a>
        </nav>
      )}
      {error ? (
        <p role="alert" className="error">
          {error}
        </p>
      ) : !file ? (
        <p role="status">正在加载媒体文件…</p>
      ) : (
        <>
          <DetailHero
            title={file.match.metadata?.title || file.filename}
            posterTitle={
              file.match.metadata?.title ||
              file.filename.replace(/\.[^.]+$/, "")
            }
            match={file.match}
            kind={file.kind === "movie" ? "电影" : "电视剧文件"}
            facts={
              <>
                {file.episode && (
                  <span>
                    第 {file.episode.seasonNumber} 季 · 第{" "}
                    {file.episode.episodeNumber} 集
                  </span>
                )}
                {format && <span className="detail-tag">{format}</span>}
                <span>{fileSize(file.size)}</span>
                {progress?.duration ? (
                  <span>{clock(progress.duration)}</span>
                ) : null}
              </>
            }
          >
            {!deleted && (
              <ExternalPlayer
                compact
                key={`external-${file.id}`}
                id={file.id}
              />
            )}
            <div className="detail-actions">
              <button
                className="primary detail-play"
                disabled={deleted}
                onClick={() => {
                  setFromStart(false);
                  setPlay(true);
                }}
              >
                <Icon name="play" /> 网页播放
              </button>
              {resume && (
                <button
                  className="secondary"
                  disabled={deleted}
                  onClick={() => {
                    setFromStart(true);
                    setPlay(true);
                  }}
                >
                  <Icon name="play" /> 从头开始
                </button>
              )}
              {progress?.completed && (
                <span className="detail-watched">
                  <Icon name="check" /> 已看完
                </span>
              )}
              <FavoriteButton
                kind="media"
                id={file.id}
                title={file.match.metadata?.title || file.filename}
                value={file.favorite}
                onChange={(state) =>
                  setFile((current) =>
                    current ? { ...current, ...state } : current,
                  )
                }
              />
              <Matching
                compact
                target={{
                  kind: file.kind,
                  id: file.episode?.seriesId || file.id,
                  title:
                    file.match.metadata?.title ||
                    file.episode?.seriesTitle ||
                    file.filename.replace(/\.[^.]+$/, ""),
                }}
              />
              <DeleteSourceFile
                compact
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
            </div>
            {resume && (
              <div className="detail-progress">
                {progress.duration > 0 && (
                  <progress
                    aria-label="观看进度"
                    max={progress.duration}
                    value={Math.min(progress.position, progress.duration)}
                  />
                )}
                <span>
                  从 {clock(progress.position)} 继续
                  {progress.duration > progress.position &&
                    ` · 剩余 ${clock(progress.duration - progress.position)}`}
                </span>
              </div>
            )}
            {!file.available && (
              <p role="status" className="notice">
                文件不可用：已删除或源文件发生变化
              </p>
            )}
          </DetailHero>
          {playerVisible && (
            <Player
              key={`${file.id}-${fromStart}`}
              file={file}
              fromStart={fromStart}
              onClose={() => {
                setPlay(false);
                const url = new URL(window.location.href);
                url.searchParams.delete("play");
                window.history.replaceState(null, "", url);
                void api<MediaFile>(`/api/media/${encodeURIComponent(id)}`)
                  .then(setFile)
                  .catch((error) => setError(message(error)));
              }}
            />
          )}
          <section className="detail-section" aria-label="媒体信息">
            <h2>媒体信息</h2>
            <p className="detail-source-path">{file.path}</p>
            <div className="detail-file-tags">
              {format && <span className="detail-tag">{format}</span>}
              <span>{fileSize(file.size)}</span>
              <span>
                修改于 {new Date(file.modifiedAt * 1000).toLocaleString()}
              </span>
            </div>
            <div className="media-info-grid">
              <section className="media-info-block">
                <h3>
                  <Icon name="film" /> 文件
                </h3>
                <dl>
                  <dt>媒体类型</dt>
                  <dd>{file.kind === "movie" ? "电影" : "电视剧文件"}</dd>
                  <dt>文件状态</dt>
                  <dd>
                    {file.available && !deleted
                      ? "上次完整扫描时可用"
                      : "文件不可用：已删除或源文件发生变化"}
                  </dd>
                  <dt>文件大小</dt>
                  <dd>{file.size.toLocaleString()} 字节</dd>
                  <dt>播放方式</dt>
                  <dd>原始视频直放 · 不转码</dd>
                </dl>
              </section>
              <section className="media-info-block">
                <h3>
                  <Icon name="folder" /> 来源
                </h3>
                <dl>
                  <dt>原始文件</dt>
                  <dd>{file.filename}</dd>
                  {file.episode && (
                    <>
                      <dt>所属电视剧</dt>
                      <dd>
                        <a href={seriesURL(file.episode.seriesId)}>
                          {file.episode.seriesTitle}
                        </a>
                      </dd>
                      <dt>季与单集</dt>
                      <dd>
                        第 {file.episode.seasonNumber} 季 · 第{" "}
                        {file.episode.episodeNumber} 集
                      </dd>
                    </>
                  )}
                  <dt>媒体目录</dt>
                  <dd className="file-path">{file.directoryPath}</dd>
                  <dt>媒体文件标识</dt>
                  <dd className="file-path">{file.id}</dd>
                </dl>
              </section>
            </div>
            {file.organizationReason && (
              <p className="notice">识别说明：{file.organizationReason}</p>
            )}
            <p className="fine detail-file-note">
              文件状态来自最近一次成功扫描或删除更新；原文件变更后请重新扫描。
              <br />
              浏览器兼容性取决于容器与编码；无法直放时可使用外部播放器。
            </p>
          </section>
        </>
      )}
    </>
  );
}
