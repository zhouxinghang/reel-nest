import { useEffect, useState } from "react";
import { api, message } from "./api";
import { libraryURL, type SeriesDetail, type SeriesSummary } from "./media";

import { Matching } from "./matching";
import { Poster } from "./metadata";
import { DetailHero } from "./detail";
import { Icon } from "./icons";
import { FavoriteButton } from "./favorite";

export function seriesURL(id: string, season?: number): string {
  return `/series/${encodeURIComponent(id)}${season === undefined ? "" : `?season=${season}`}`;
}

export function SeriesCard({ series }: { series: SeriesSummary }) {
  return (
    <a
      href={seriesURL(series.id)}
      className="media-card series-card"
      title={`${series.match.metadata?.title || series.title} · ${series.directoryPath}`}
    >
      <Poster
        metadata={series.match.metadata}
        title={series.match.metadata?.title || series.title}
      >
        <span
          className="poster-badge"
          role="img"
          aria-label={`${series.episodeCount} 集`}
        >
          {series.episodeCount}
        </span>
        <span className="poster-hover" aria-hidden="true">
          <Icon name="arrow" />
          <span>查看详情</span>
        </span>
      </Poster>
      <div className="card-heading">
        <h2>{series.match.metadata?.title || series.title}</h2>
        {series.availableFileCount === 0 && (
          <span className="unavailable">不可用</span>
        )}
      </div>
      <p>
        {series.seasonCount} 季 · {series.episodeCount} 集 · {series.fileCount}{" "}
        个文件
      </p>
      <p className="series-root" title={series.directoryPath}>
        {series.directoryPath}
      </p>
    </a>
  );
}

export function SeriesDetails({ id }: { id: string }) {
  const [series, setSeries] = useState<SeriesDetail | null>(null);
  const [error, setError] = useState("");
  useEffect(() => {
    const refresh = () => {
      void api<SeriesDetail>(`/api/series/${encodeURIComponent(id)}`)
        .then(setSeries)
        .catch((error) => setError(message(error)));
    };
    refresh();
    window.addEventListener("metadata-updated", refresh);
    return () => window.removeEventListener("metadata-updated", refresh);
  }, [id]);
  const requested = new URLSearchParams(window.location.search).get("season");
  const season =
    requested === null
      ? series?.seasons[0]
      : /^\d+$/.test(requested)
        ? series?.seasons.find((item) => item.number === Number(requested))
        : undefined;
  return (
    <>
      {error ? (
        <p role="alert" className="error">
          {error}
        </p>
      ) : !series ? (
        <p role="status">正在加载电视剧…</p>
      ) : (
        <>
          <DetailHero
            title={series.match.metadata?.title || series.title}
            match={series.match}
            kind="电视剧"
            facts={
              <>
                <span>{series.seasonCount} 季</span>
                <span>{series.episodeCount} 集</span>
                <span>{series.fileCount} 个媒体文件</span>
              </>
            }
          >
            <div className="detail-actions">
              <a className="primary detail-play" href="#seasons">
                <Icon name="list" /> 浏览单集
              </a>
              <FavoriteButton
                kind="series"
                id={series.id}
                title={series.match.metadata?.title || series.title}
                value={series.favorite}
                onChange={(state) =>
                  setSeries((current) =>
                    current ? { ...current, ...state } : current,
                  )
                }
              />
              <Matching
                compact
                target={{
                  kind: "series",
                  id: series.id,
                  title: series.match.metadata?.title || series.title,
                }}
              />
            </div>
            {series.availableFileCount === 0 && (
              <p className="notice">
                当前没有可用的媒体文件，请检查媒体目录并重新扫描。
              </p>
            )}
          </DetailHero>
          <section
            className="detail-section series-seasons"
            id="seasons"
            aria-label="季与单集"
          >
            <h2>季与单集</h2>
            <p className="notice">
              季与单集按本地文件名组织，不受影片资料匹配影响。同一单集的不同文件版本分别保留，请选择具体文件查看详情。
            </p>
            <nav className="season-nav" aria-label="季导航">
              {series.seasons.map((item) => (
                <a
                  className={season?.number === item.number ? "selected" : ""}
                  aria-current={
                    season?.number === item.number ? "page" : undefined
                  }
                  href={seriesURL(id, item.number)}
                  key={item.number}
                >
                  第 {item.number} 季
                </a>
              ))}
            </nav>
            {!season ? (
              <p role="alert" className="notice">
                所选季不存在，请选择上方已有的季。
              </p>
            ) : (
              <section
                className="season-content"
                aria-label={`第 ${season.number} 季单集`}
              >
                <div className="season-heading">
                  <h2>第 {season.number} 季</h2>
                  <span className="fine">
                    {season.episodes.length} 集
                    {season.number === 0 ? " · 特别篇" : ""}
                  </span>
                </div>
                {season.episodes.map((episode) => (
                  <article className="episode" key={episode.number}>
                    <div className="episode-heading">
                      <h3>第 {episode.number} 集</h3>
                      <span className="fine">
                        {episode.files.length} 个文件
                        {episode.files.length > 1 ? " · 不同版本独立保留" : ""}
                      </span>
                    </div>
                    <div className="episode-files">
                      {episode.files.map((file, index) => (
                        <a
                          className="episode-file"
                          href={`/media/${file.id}`}
                          key={file.id}
                        >
                          <span
                            className="episode-file-number"
                            aria-hidden="true"
                          >
                            {String(index + 1).padStart(2, "0")}
                          </span>
                          <div className="episode-file-info">
                            <span className="episode-filename">
                              {file.filename}
                            </span>
                            <span className="file-path">
                              {file.relativePath}
                            </span>
                            <span className="fine">
                              {file.size.toLocaleString()} 字节 ·{" "}
                              {file.available ? "上次扫描时可用" : "文件不可用"}
                            </span>
                          </div>
                          <span className="episode-detail-label">
                            查看详情 →
                          </span>
                        </a>
                      ))}
                    </div>
                  </article>
                ))}
              </section>
            )}
          </section>
          <section className="detail-section" aria-label="电视剧媒体信息">
            <h2>媒体信息</h2>
            <p className="fine">
              所属媒体库：
              <a href={libraryURL(series.directoryId)}>
                {series.directoryName}
              </a>
            </p>
            <p className="fine">来源目录</p>
            <p className="detail-source-path">{series.directoryPath}</p>
            <p className="fine">
              {series.fileCount} 个媒体文件 · {series.availableFileCount} 个可用
            </p>
          </section>
        </>
      )}
    </>
  );
}
