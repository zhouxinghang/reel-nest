import { useEffect, useRef, useState, type ReactNode } from "react";
import { api, message } from "./api";
import {
  libraryURL,
  type Directory,
  type MediaFile,
  type SeriesSummary,
} from "./media";
import { Poster } from "./metadata";
import { Icon } from "./icons";
import { FavoriteButton, type FavoriteState } from "./favorite";
import { clock } from "./player";

export type LibraryFilter =
  | "all"
  | "movie"
  | "series"
  | "unmatched"
  | "favorites"
  | "recent";
export function readLibraryFilter(): LibraryFilter {
  const value = new URLSearchParams(window.location.search).get("type");
  return value === "movie" ||
    value === "series" ||
    value === "unmatched" ||
    value === "favorites" ||
    value === "recent"
    ? value
    : "all";
}

type Entry = {
  id: string;
  directoryId: string;
  kind: "media" | "series";
  title: string;
  href: string;
  addedAt: number;
  favorite: boolean;
  favoritedAt: number;
  metadata: MediaFile["match"]["metadata"];
  available: boolean;
  file?: MediaFile;
  series?: SeriesSummary;
};
function fileEntry(file: MediaFile): Entry {
  return {
    ...file,
    kind: "media",
    title: file.match.metadata?.title || file.filename,
    href: `/media/${file.id}`,
    metadata: file.match.metadata,
    file,
  };
}
function seriesEntry(series: SeriesSummary): Entry {
  return {
    ...series,
    kind: "series",
    title: series.match.metadata?.title || series.title,
    href: `/series/${series.id}`,
    metadata: series.match.metadata,
    available: series.availableFileCount > 0,
    series,
  };
}
function newest(a: Entry, b: Entry) {
  return b.addedAt - a.addedAt || a.id.localeCompare(b.id);
}
function addedLabel(time: number) {
  return time > 0
    ? `添加于 ${new Date(time).toLocaleString()}`
    : "收录时间未知（旧版数据）";
}

function Shelf({
  title,
  more,
  children,
}: {
  title: string;
  more?: string;
  children: ReactNode;
}) {
  const row = useRef<HTMLDivElement>(null);
  const [edges, setEdges] = useState({ start: true, end: true });
  useEffect(() => {
    const element = row.current!;
    const measure = () =>
      setEdges({
        start: element.scrollLeft < 2,
        end:
          element.scrollLeft + element.clientWidth >= element.scrollWidth - 2,
      });
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    measure();
    element.addEventListener("scroll", measure);
    return () => {
      observer.disconnect();
      element.removeEventListener("scroll", measure);
    };
  }, [children]);
  return (
    <section className="home-shelf" aria-label={title}>
      <div className="shelf-heading">
        <h2>
          {more ? (
            <a href={more}>
              {title}
              <Icon name="chevron" />
            </a>
          ) : (
            title
          )}
        </h2>
        <div className="shelf-arrows">
          <button
            className="icon-button"
            aria-label={`向左浏览${title}`}
            disabled={edges.start}
            onClick={() =>
              row.current?.scrollBy({
                left: -row.current.clientWidth * 0.85,
                behavior: "smooth",
              })
            }
          >
            <Icon name="chevron" className="left-chevron" />
          </button>
          <button
            className="icon-button"
            aria-label={`向右浏览${title}`}
            disabled={edges.end}
            onClick={() =>
              row.current?.scrollBy({
                left: row.current.clientWidth * 0.85,
                behavior: "smooth",
              })
            }
          >
            <Icon name="chevron" />
          </button>
        </div>
      </div>
      <div className="home-shelf-row" ref={row}>
        {children}
      </div>
    </section>
  );
}

function Card({
  entry,
  recent = false,
  onFavorite,
}: {
  entry: Entry;
  recent?: boolean;
  onFavorite: (entry: Entry, state: FavoriteState) => void;
}) {
  return (
    <article className="home-media-card" data-added-at={entry.addedAt}>
      <a
        href={entry.href}
        className={`media-card${entry.series ? " series-card" : ""}`}
        title={entry.title}
      >
        <Poster
          metadata={entry.metadata}
          title={entry.title.replace(/\.[^.]+$/, "")}
        >
          {entry.series && (
            <span
              className="poster-badge"
              role="img"
              aria-label={`${entry.series.episodeCount} 集`}
            >
              {entry.series.episodeCount}
            </span>
          )}
          {entry.file?.progress?.completed && (
            <span
              className="poster-badge watched"
              role="img"
              aria-label="已看完"
            >
              <Icon name="check" />
            </span>
          )}
          <span className="poster-hover" aria-hidden="true">
            <Icon name="arrow" />
          </span>
        </Poster>
        <div className="card-heading">
          <h2>{entry.title}</h2>
          {!entry.available && <span className="unavailable">不可用</span>}
        </div>
        <p>
          {entry.file?.episode
            ? `第 ${entry.file.episode.seasonNumber} 季 · 第 ${entry.file.episode.episodeNumber} 集`
            : entry.metadata?.year ||
              (entry.series
                ? `${entry.series.seasonCount} 季 · ${entry.series.episodeCount} 集`
                : "未匹配媒体")}
        </p>
        {recent && <p className="card-added">{addedLabel(entry.addedAt)}</p>}
      </a>
      <FavoriteButton
        kind={entry.kind}
        id={entry.id}
        title={entry.title}
        value={entry.favorite}
        onChange={(state) => onFavorite(entry, state)}
      />
    </article>
  );
}

export function Library({ filter }: { filter: LibraryFilter }) {
  const directoryId =
    new URLSearchParams(window.location.search).get("directory") || "";
  const [directories, setDirectories] = useState<Directory[]>([]);
  const [items, setItems] = useState<MediaFile[]>([]);
  const [series, setSeries] = useState<SeriesSummary[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [error, setError] = useState("");
  const [attempt, setAttempt] = useState(0);
  const [feedback, setFeedback] = useState("");
  const [query, setQuery] = useState("");
  const [category, setCategory] = useState("all");
  const [view, setView] = useState("poster");
  const [sort, setSort] = useState("recent");
  const libraryRevision = useRef(0);
  const searchMode =
    new URLSearchParams(window.location.search).get("focus") === "search";
  useEffect(() => {
    const controller = new AbortController();
    const refresh = () => {
      const revision = ++libraryRevision.current;
      void api<{
        items: MediaFile[];
        series: SeriesSummary[];
        directories: Directory[];
      }>(
        `/api/library?sort=recent&directoryId=${encodeURIComponent(directoryId)}`,
        undefined,
        { signal: controller.signal },
      )
        .then((data) => {
          if (
            !controller.signal.aborted &&
            revision === libraryRevision.current
          ) {
            setDirectories(data.directories);
            setItems(data.items);
            setSeries(data.series);
            setLoaded(true);
            setError("");
          }
        })
        .catch((error) => {
          if (
            !controller.signal.aborted &&
            revision === libraryRevision.current
          )
            setError(message(error));
        });
    };
    refresh();
    window.addEventListener("metadata-updated", refresh);
    window.addEventListener("focus", refresh);
    return () => {
      controller.abort();
      window.removeEventListener("metadata-updated", refresh);
      window.removeEventListener("focus", refresh);
    };
  }, [attempt, directoryId]);
  function onFavorite(entry: Entry, state: FavoriteState) {
    // A refresh started before this mutation must not restore stale favorites.
    libraryRevision.current++;
    if (entry.kind === "series")
      setSeries((current) =>
        current.map((item) =>
          item.id === entry.id ? { ...item, ...state } : item,
        ),
      );
    else
      setItems((current) =>
        current.map((item) =>
          item.id === entry.id ? { ...item, ...state } : item,
        ),
      );
    setFeedback(`${state.favorite ? "已收藏" : "已取消收藏"}：${entry.title}`);
  }
  const home = filter === "all" && !searchMode && !query && !directoryId;
  const selectedDirectory = directories.find(
    (directory) => directory.id === directoryId,
  );
  const titles: Record<LibraryFilter, string> = {
    all: "媒体库",
    movie: "电影",
    series: "电视剧",
    unmatched: "未匹配媒体",
    favorites: "收藏",
    recent: "最近添加",
  };
  const showFiles = items.filter((file) => {
    if (filter === "favorites") return file.favorite;
    if (filter === "unmatched") return !file.matched;
    return !file.episode;
  });
  const showSeries =
    filter === "unmatched"
      ? []
      : series.filter((item) => filter !== "favorites" || item.favorite);
  const queryText = query.trim().toLocaleLowerCase();
  // A grouped series must still be discoverable by any episode's filename.
  const matchingSeries = new Set(
    items
      .filter((file) => file.filename.toLocaleLowerCase().includes(queryText))
      .map((file) => file.episode?.seriesId),
  );
  const entries = [...showFiles.map(fileEntry), ...showSeries.map(seriesEntry)]
    .filter(
      (entry) =>
        (filter !== "movie" || entry.file?.kind === "movie") &&
        (filter !== "series" ||
          entry.kind === "series" ||
          entry.file?.kind === "series"),
    )
    .filter(
      (entry) =>
        category === "all" ||
        (category === "movie"
          ? entry.file?.kind === "movie"
          : entry.series || entry.file?.kind === "series"),
    )
    .filter(
      (entry) =>
        [
          entry.title,
          entry.file?.filename,
          entry.file?.episode?.seriesTitle,
          entry.series?.title,
        ].some((title) => title?.toLocaleLowerCase().includes(queryText)) ||
        (entry.kind === "series" && matchingSeries.has(entry.id)),
    )
    .sort((a, b) =>
      sort === "title"
        ? a.title.localeCompare(b.title, "zh-CN", { numeric: true }) ||
          a.id.localeCompare(b.id)
        : filter === "favorites"
          ? b.favoritedAt - a.favoritedAt || a.id.localeCompare(b.id)
          : newest(a, b),
    );
  const collections = directories.map((directory) => ({
    directory,
    entries: [
      ...items
        .filter((file) => file.directoryId === directory.id && !file.episode)
        .map(fileEntry),
      ...series
        .filter((show) => show.directoryId === directory.id)
        .map(seriesEntry),
    ].sort(newest),
  }));
  // Keep the selected sort within each group; order groups by their first result.
  const groups = new Map<string, Entry[]>();
  for (const entry of entries) {
    const group = groups.get(entry.directoryId) || [];
    group.push(entry);
    groups.set(entry.directoryId, group);
  }
  const continuing = items
    .filter(
      (file) =>
        file.available &&
        file.progress &&
        file.progress.position > 0 &&
        !file.progress.completed,
    )
    .sort(
      (a, b) =>
        b.progress!.updatedAt - a.progress!.updatedAt ||
        a.id.localeCompare(b.id),
    );
  return (
    <div className="emby-library">
      <h1 className={home ? "sr-only" : "browse-heading"}>
        {selectedDirectory?.name || (searchMode ? "搜索" : titles[filter])}
      </h1>
      {feedback && (
        <p className="sr-only" role="status">
          {feedback}
        </p>
      )}
      {error ? (
        <div>
          <p className="error" role="alert">
            {error}
          </p>
          <button
            className="secondary"
            onClick={() => setAttempt((value) => value + 1)}
          >
            重新加载媒体库
          </button>
          {directoryId && (
            <a className="back-link" href="/library">
              返回全部媒体库
            </a>
          )}
        </div>
      ) : !loaded ? (
        <p role="status" className="library-loading">
          正在加载媒体库…
        </p>
      ) : (
        <>
          {home && (
            <>
              <section className="home-libraries" aria-label="我的媒体">
                <h2>我的媒体</h2>
                <div className="home-library-tiles">
                  {collections.map(({ directory, entries: collection }) => (
                    <a
                      key={directory.id}
                      className={`home-library-tile tile-${directory.kind}`}
                      aria-label={`浏览媒体库：${directory.name}`}
                      title={directory.path}
                      href={libraryURL(directory.id)}
                    >
                      <span className="library-tile-art" aria-hidden="true">
                        {collection.some((entry) => entry.metadata) ? (
                          <Poster
                            landscape
                            metadata={
                              collection.find(
                                (entry) =>
                                  entry.metadata?.thumbUrl ||
                                  entry.metadata?.backdropUrl,
                              )?.metadata ||
                              collection.find((entry) => entry.metadata)
                                ?.metadata
                            }
                            title={directory.name}
                          />
                        ) : (
                          <Icon
                            name={directory.kind === "movie" ? "film" : "tv"}
                          />
                        )}
                      </span>
                      <span className="library-tile-name">
                        {directory.name}
                      </span>
                      <span className="library-tile-description">
                        {directory.kind === "movie" ? "电影" : "电视剧"} ·{" "}
                        {collection.length} 项
                      </span>
                    </a>
                  ))}
                </div>
              </section>
              {continuing.length > 0 && (
                <Shelf title="继续观看">
                  {continuing.map((file) => (
                    <a
                      key={file.id}
                      className="continue-card"
                      href={`/media/${file.id}?play=1`}
                    >
                      <div className="continue-art">
                        <Poster
                          landscape
                          metadata={file.match.metadata}
                          title={file.match.metadata?.title || file.filename}
                        />
                        <span className="continue-play" aria-hidden="true">
                          <Icon name="play" />
                        </span>
                        <span className="continue-time">
                          {clock(file.progress!.position)} /{" "}
                          {clock(file.progress!.duration)}
                        </span>
                        <progress
                          className="watch-progress"
                          aria-label={`${file.filename} 观看进度`}
                          max={Math.max(1, file.progress!.duration)}
                          value={file.progress!.position}
                        />
                      </div>
                      <strong>
                        {file.match.metadata?.title || file.filename}
                      </strong>
                      <span className="fine">
                        {file.episode
                          ? `第 ${file.episode.seasonNumber} 季 · 第 ${file.episode.episodeNumber} 集`
                          : file.match.metadata?.year || "电影"}
                      </span>
                    </a>
                  ))}
                </Shelf>
              )}
              {collections
                .filter((collection) => collection.entries.length > 0)
                .map(({ directory, entries: collection }) => (
                  <Shelf
                    key={directory.id}
                    title={`最近添加 · ${directory.name}`}
                    more={libraryURL(directory.id)}
                  >
                    {collection.slice(0, 20).map((entry) => (
                      <Card
                        key={`${entry.kind}-${entry.id}`}
                        entry={entry}
                        onFavorite={onFavorite}
                      />
                    ))}
                  </Shelf>
                ))}
            </>
          )}
          {!home && (
            <>
              {selectedDirectory && (
                <div className="library-source">
                  <a className="back-link" href="/library">
                    ← 全部媒体库
                  </a>
                  <p className="fine file-path">{selectedDirectory.path}</p>
                  <p className="fine">
                    {selectedDirectory.kind === "movie" ? "电影" : "电视剧"}
                    媒体库
                  </p>
                  {selectedDirectory.scanError && (
                    <p className="notice">{selectedDirectory.scanError}</p>
                  )}
                </div>
              )}
              <div className="home-browse-toolbar">
                <select
                  aria-label="选择媒体库"
                  value={directoryId}
                  onChange={(event) => {
                    const url = new URL(window.location.href);
                    if (event.target.value)
                      url.searchParams.set("directory", event.target.value);
                    else url.searchParams.delete("directory");
                    window.location.assign(url);
                  }}
                >
                  <option value="">全部媒体库</option>
                  {directories.map((directory) => (
                    <option key={directory.id} value={directory.id}>
                      {directory.name} · {directory.path}
                    </option>
                  ))}
                </select>
                <div className="search-field">
                  <Icon name="search" />
                  <input
                    aria-label="搜索文件名"
                    type="search"
                    placeholder="搜索标题或文件名"
                    value={query}
                    onChange={(event) => setQuery(event.target.value)}
                    autoFocus={searchMode}
                  />
                </div>
                {!selectedDirectory &&
                  (filter === "favorites" ||
                    filter === "recent" ||
                    filter === "all") && (
                    <div className="filters" role="group" aria-label="媒体分类">
                      {[
                        ["all", "全部"],
                        ["movie", "电影"],
                        ["series", "电视剧"],
                      ].map(([value, label]) => (
                        <button
                          key={value}
                          className={category === value ? "selected" : ""}
                          aria-pressed={category === value}
                          onClick={() => setCategory(value)}
                        >
                          {label}
                        </button>
                      ))}
                    </div>
                  )}
                <div className="collection-controls">
                  <select
                    aria-label="排序方式"
                    value={sort}
                    onChange={(event) => setSort(event.target.value)}
                  >
                    <option value="recent">
                      {filter === "favorites" ? "最近收藏" : "最近添加"}
                    </option>
                    <option value="title">标题 A–Z</option>
                  </select>
                  <div
                    className="view-controls"
                    role="group"
                    aria-label="显示方式"
                  >
                    <button
                      aria-label="海报视图"
                      aria-pressed={view === "poster"}
                      onClick={() => setView("poster")}
                    >
                      <Icon name="library" />
                    </button>
                    <button
                      aria-label="列表视图"
                      aria-pressed={view === "list"}
                      onClick={() => setView("list")}
                    >
                      <Icon name="list" />
                    </button>
                  </div>
                </div>
              </div>
              <section aria-label="媒体收藏">
                <p className="browse-count">
                  {entries.length} 项
                  {filter === "favorites" ? "收藏 · 家庭共享" : "媒体"}
                </p>
                {entries.length ? (
                  [...groups].map(([id, collection]) => {
                    const directory = directories.find(
                      (item) => item.id === id,
                    )!;
                    return (
                      <section
                        className="library-group"
                        key={id}
                        aria-label={`媒体库：${directory.name}`}
                      >
                        {!selectedDirectory && (
                          <>
                            <div className="library-group-heading">
                              <h2>
                                <a href={libraryURL(id)}>{directory.name}</a>
                              </h2>
                              <span className="fine">
                                {collection.length} 项
                              </span>
                            </div>
                            <p className="fine file-path">{directory.path}</p>
                          </>
                        )}
                        <div
                          className={`home-card-grid${view === "list" ? " home-card-list" : ""}`}
                        >
                          {collection.map((entry) => (
                            <Card
                              key={`${entry.kind}-${entry.id}`}
                              entry={entry}
                              recent={filter === "recent"}
                              onFavorite={onFavorite}
                            />
                          ))}
                        </div>
                      </section>
                    );
                  })
                ) : (
                  <div className="home-empty">
                    <Icon name={filter === "favorites" ? "heart" : "search"} />
                    <h2>
                      {filter === "favorites" && !query && category === "all"
                        ? "还没有收藏"
                        : selectedDirectory && items.length === 0
                          ? "这个媒体库还是空的"
                          : "没有找到对应的媒体"}
                    </h2>
                    <p>
                      {filter === "favorites"
                        ? "点击海报或详情页上的爱心，将喜欢的电影与电视剧放在这里。"
                        : selectedDirectory && items.length === 0
                          ? "首次扫描可能仍在进行；扫描完成后刷新页面，或到媒体目录检查扫描状态。"
                          : "试试其他标题或分类。"}
                    </p>
                    <a className="secondary" href="/library">
                      返回首页
                    </a>
                  </div>
                )}
              </section>
            </>
          )}
          {home && directories.length === 0 && (
            <section className="home-empty">
              <Icon name="film" />
              <h2>媒体库还是空的</h2>
              <p>接入媒体目录，开始添加你的电影和电视剧。</p>
              <a className="primary" href="/directories">
                接入第一个媒体目录
              </a>
            </section>
          )}
        </>
      )}
    </div>
  );
}
