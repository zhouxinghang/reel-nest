import { useState } from "react";
import { Matching } from "../matching/Matching";
import { Player } from "../playback/Player";
import { External } from "../playback/External";
import { Deletion } from "../playback/Deletion";
import { useQuery } from "@tanstack/react-query";
import { Link, useParams, useSearchParams } from "react-router-dom";
import type { Schema } from "../../shared/api";
import { useAPI } from "../../shared/services";
import {
  Empty,
  Loading,
  Pagination,
  QueryError,
  ui,
  useStaleCursor,
} from "../../shared/ui";
import { Cards, Favorite } from "./Card";
import { queries } from "./queries";
import styles from "./catalog.module.css";

function Artwork({
  id,
  className = "",
}: {
  id: string | null | undefined;
  className?: string;
}) {
  return id ? (
    <img
      key={id}
      className={className}
      src={`/api/assets/${encodeURIComponent(id)}/content`}
      alt=""
      loading="lazy"
      onError={(e) => {
        e.currentTarget.hidden = true;
      }}
    />
  ) : null;
}
function Match({ match }: { match: Schema["Match"] }) {
  const metadata = match.metadata;
  return (
    <section aria-label="影片资料">
      <h2>影片资料</h2>
      {metadata ? (
        <>
          <p>{metadata.overview || "暂无简介"}</p>
          <p className={ui.muted}>
            来源：{metadata.summary.source} · 有效至{" "}
            {new Date(metadata.summary.expiresAt).toLocaleString()}
          </p>
          <h3>演职员</h3>
          <div className={styles.credits}>
            {[...metadata.credits.directors, ...metadata.credits.actors].map(
              (credit) => (
                <Link
                  key={`${credit.personId}-${credit.role}`}
                  to={`/people/${encodeURIComponent(credit.personId)}`}
                >
                  <Artwork
                    id={credit.photoAssetId}
                    className={styles.portrait}
                  />
                  {credit.name} ·{" "}
                  {credit.role === "director"
                    ? "导演"
                    : credit.characters.join(" / ") || "演员"}
                </Link>
              ),
            )}
          </div>
          {metadata.credits.state !== "ready" && (
            <p className={ui.muted}>演职员资料尚未完整，可稍后刷新查看。</p>
          )}
        </>
      ) : (
        <p className={ui.muted}>
          {match.cacheState === "expired"
            ? "资料已过期，等待刷新；已选关联仍然保留。"
            : "未匹配影片资料。"}
        </p>
      )}
    </section>
  );
}
export function MediaDetail() {
  const { id = "" } = useParams();
  return <MediaContent key={id} id={id} />;
}
function MediaContent({ id }: { id: string }) {
  const query = useQuery(queries.media(useAPI(), id));
  const [modal, setModal] = useState<
    "match" | "play" | "start" | "delete" | null
  >(null);
  const [notice, setNotice] = useState("");
  return (
    <>
      <QueryError error={query.error} retry={() => void query.refetch()} />
      {query.isPending && <Loading />}
      {query.data && (
        <>
          <Artwork
            id={query.data.card.metadata?.artwork.backdrop}
            className={styles.backdrop}
          />
          <div className={styles.hero}>
            <Artwork
              id={query.data.card.metadata?.artwork.primary}
              className={styles.detailPoster}
            />
            <div>
              <h1>{query.data.card.title}</h1>
              <p className={ui.muted}>
                {query.data.card.metadata?.year} ·{" "}
                {query.data.card.kind === "movie" ? "电影" : "电视剧"}
              </p>
            </div>
          </div>
          <Favorite card={query.data.card} />
          <p className={styles.path}>{query.data.path}</p>
          <p className={ui.muted}>
            {query.data.size.toLocaleString()} 字节 ·{" "}
            {new Date(query.data.modifiedAt).toLocaleString()}
            {query.data.card.state === "deleted" && " · 源文件已删除"}
          </p>
          {query.data.card.episode && (
            <Link
              to={`/series/${encodeURIComponent(query.data.card.episode.seriesId)}?season=${query.data.card.episode.seasonNumber}`}
            >
              返回电视剧 · 第 {query.data.card.episode.seasonNumber} 季
            </Link>
          )}
          <div className={styles.seasons}>
            <button
              className={ui.primary}
              disabled={query.data.card.state === "deleted"}
              onClick={() => setModal("play")}
            >
              {query.data.card.progress && !query.data.card.progress.completed
                ? "继续播放"
                : "网页播放"}
            </button>
            <button
              className={ui.button}
              disabled={query.data.card.state === "deleted"}
              onClick={() => setModal("start")}
            >
              从头播放
            </button>
            <button className={ui.button} onClick={() => setModal("match")}>
              匹配影片资料
            </button>
            <button
              className={ui.button}
              disabled={query.data.card.state === "deleted"}
              onClick={() => setModal("delete")}
            >
              删除源文件
            </button>
          </div>
          {notice && <p role="status">{notice}</p>}
          {modal === "match" && (
            <Matching
              match={query.data.match}
              libraryId={query.data.card.libraryId}
              title={query.data.card.title}
              onClose={() => setModal(null)}
            />
          )}
          {(modal === "play" || modal === "start") && (
            <Player
              id={id}
              title={query.data.card.title}
              libraryId={query.data.card.libraryId}
              fromStart={modal === "start"}
              onClose={(text) => {
                setNotice(text);
                setModal(null);
              }}
            />
          )}
          {modal === "delete" && (
            <Deletion
              id={id}
              subjectId={query.data.match.subjectId}
              libraryId={query.data.card.libraryId}
              onClose={(text) => {
                setNotice(text);
                setModal(null);
              }}
            />
          )}
          {query.data.card.state !== "deleted" && <External id={id} />}
          <Match match={query.data.match} />
        </>
      )}
    </>
  );
}
function Episodes({ id, season }: { id: string; season: number }) {
  const [search] = useSearchParams();
  const query = useQuery(
    queries.episodes(useAPI(), id, season, search.get("episodes") || ""),
  );
  useStaleCursor(query.error, "episodes");
  return (
    <section aria-label={`第 ${season} 季单集`}>
      <h2>第 {season} 季</h2>
      <QueryError error={query.error} retry={() => void query.refetch()} />
      {query.isPending && <Loading />}
      {query.data && (
        <>
          <Cards items={query.data.items.map((item) => item.file)} />
          {!query.data.items.length && <Empty />}
          <Pagination page={query.data.page} cursorKey="episodes" />
        </>
      )}
    </section>
  );
}
export function SeriesDetail() {
  const { id = "" } = useParams();
  return <SeriesContent key={id} id={id} />;
}
function SeriesContent({ id }: { id: string }) {
  const [matching, setMatching] = useState(false);
  const [search, setSearch] = useSearchParams();
  const query = useQuery(
    queries.series(useAPI(), id, search.get("cursor") || ""),
  );
  useStaleCursor(query.error);
  const raw = search.get("season");
  const selected =
    raw !== null && /^\d+$/.test(raw) && Number.isSafeInteger(Number(raw))
      ? Number(raw)
      : query.data?.seasons[0]?.number;
  return (
    <>
      <QueryError error={query.error} retry={() => void query.refetch()} />
      {query.isPending && <Loading />}
      {query.data && (
        <>
          <Artwork
            id={query.data.card.metadata?.artwork.backdrop}
            className={styles.backdrop}
          />
          <div className={styles.hero}>
            <Artwork
              id={query.data.card.metadata?.artwork.primary}
              className={styles.detailPoster}
            />
            <div>
              <h1>{query.data.card.title}</h1>
              <p className={ui.muted}>
                {query.data.card.metadata?.year} · 电视剧
              </p>
            </div>
          </div>
          <Favorite card={query.data.card} />
          <p className={ui.muted}>
            {query.data.card.seasonCount} 季 · {query.data.card.episodeCount} 集
            · {query.data.card.fileCount} 个文件版本
          </p>
          <button className={ui.button} onClick={() => setMatching(true)}>
            匹配影片资料
          </button>
          {matching && (
            <Matching
              match={query.data.match}
              libraryId={query.data.card.libraryId}
              title={query.data.card.title}
              onClose={() => setMatching(false)}
            />
          )}
          <Match match={query.data.match} />
          <nav className={styles.seasons} aria-label="季">
            {query.data.seasons.map((season) => (
              <button
                key={season.number}
                className={ui.button}
                aria-pressed={selected === season.number}
                onClick={() => {
                  const next = new URLSearchParams(search);
                  next.set("season", String(season.number));
                  next.delete("episodes");
                  setSearch(next);
                }}
              >
                第 {season.number} 季
              </button>
            ))}
          </nav>
          <Pagination page={query.data.page} />
          {selected !== undefined && <Episodes id={id} season={selected} />}
        </>
      )}
    </>
  );
}
export function PersonDetail() {
  const { id = "" } = useParams();
  const [search] = useSearchParams();
  const api = useAPI();
  const person = useQuery(queries.person(api, id));
  const works = useQuery(queries.works(api, id, search.get("cursor") || ""));
  useStaleCursor(works.error);
  return (
    <>
      <QueryError error={person.error} retry={() => void person.refetch()} />
      {person.isPending && <Loading />}
      {person.data && (
        <>
          <div className={styles.hero}>
            <Artwork
              id={person.data.photoAssetId}
              className={styles.detailPoster}
            />
            <h1>{person.data.name}</h1>
          </div>
          <p className={ui.muted}>
            来源：{person.data.source} · 当前媒体库中的作品
          </p>
        </>
      )}
      <QueryError error={works.error} retry={() => void works.refetch()} />
      {works.isPending && <Loading />}
      {works.data && (
        <>
          {works.data.items.length ? (
            <Cards items={works.data.items.map((work) => work.card)} />
          ) : (
            <Empty />
          )}
          <Pagination page={works.data.page} />
        </>
      )}
    </>
  );
}
