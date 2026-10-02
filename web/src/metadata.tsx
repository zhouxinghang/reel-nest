import { useEffect, useState, type ReactNode } from "react";
import type { SourceStatus } from "./metadata-sources";
import { Icon } from "./icons";
import { api, message } from "./api";
import type { MetadataAssociation, TitleMetadata } from "./media";

export function Poster({
  metadata,
  title,
  detail = false,
  landscape = false,
  children,
}: {
  metadata?: TitleMetadata | null;
  title: string;
  detail?: boolean;
  landscape?: boolean;
  children?: ReactNode;
}) {
  const [failed, setFailed] = useState<string[]>([]);
  useEffect(
    () => setFailed([]),
    [
      metadata?.source,
      metadata?.sourceId,
      metadata?.fetchedAt,
      metadata?.artwork?.fetchedAt,
    ],
  );
  const candidates = landscape
    ? [
        { url: metadata?.thumbUrl, type: "thumb", label: "横版封面" },
        { url: metadata?.backdropUrl, type: "backdrop", label: "背景图" },
        { url: metadata?.posterUrl, type: "poster", label: "海报" },
      ]
    : [{ url: metadata?.posterUrl, type: "poster", label: "海报" }];
  const selected = candidates.find(
    (image) => image.url && !failed.includes(image.url),
  );
  return (
    <div
      className={`poster-placeholder${detail ? " detail-poster" : ""}${landscape ? " landscape-artwork" : ""}`}
      data-image-type={selected?.type || "placeholder"}
      data-tone={
        Array.from(title).reduce((sum, char) => sum + char.codePointAt(0)!, 0) %
        5
      }
    >
      {selected?.url ? (
        <img
          className="poster-image"
          key={selected.url}
          src={selected.url}
          alt={`${title} ${selected.label}`}
          loading={detail ? "eager" : "lazy"}
          decoding="async"
          onError={() => setFailed((urls) => [...urls, selected.url!])}
        />
      ) : (
        <>
          <span className="poster-top">REEL NEST</span>
          <span className="poster-symbol" aria-hidden="true">
            <Icon name="film" />
          </span>
          <span className="poster-title">{title}</span>
          <span className="poster-bottom">本地收藏</span>
        </>
      )}
      {children}
    </div>
  );
}

export function DetailBackdrop({
  metadata,
}: {
  metadata: TitleMetadata | null;
}) {
  const [failed, setFailed] = useState<string[]>([]);
  useEffect(
    () => setFailed([]),
    [
      metadata?.source,
      metadata?.sourceId,
      metadata?.fetchedAt,
      metadata?.artwork?.fetchedAt,
    ],
  );
  const url = [metadata?.backdropUrl, metadata?.thumbUrl].find(
    (candidate) => candidate && !failed.includes(candidate),
  );
  if (!url) return null;
  return (
    <div className="detail-backdrop" aria-hidden="true">
      <img
        key={url}
        src={url}
        alt=""
        decoding="async"
        onError={() => setFailed((urls) => [...urls, url])}
      />
    </div>
  );
}

export function MetadataInfo({ match }: { match: MetadataAssociation }) {
  const metadata = match.metadata;
  const [expanded, setExpanded] = useState(false);
  const longOverview = (metadata?.overview.length || 0) > 160;
  return (
    <div className="metadata-info">
      <p
        className={`detail-description${longOverview && !expanded ? " is-collapsed" : ""}`}
      >
        {metadata?.overview ||
          (metadata
            ? "暂无可用简介。"
            : "尚未关联影片资料，保留原始文件名供浏览。未匹配媒体也可以直接尝试播放，不要求在线资料源凭据。")}
      </p>
      {longOverview && (
        <button
          className="text-button overview-toggle"
          aria-expanded={expanded}
          onClick={() => setExpanded(!expanded)}
        >
          {expanded ? "收起简介" : "更多简介"}
        </button>
      )}
      {match.error && (
        <p className="notice" role="status">
          {match.error}
        </p>
      )}
      {metadata?.artwork?.error && (
        <p className="fine" role="status">
          横版图片暂不可用，已使用回退展示；稍后可刷新资料重试。
        </p>
      )}
      {metadata && (
        <p className="fine">
          {metadata.sourceUrl ? (
            <a href={metadata.sourceUrl} target="_blank" rel="noreferrer">
              资料来源：{metadata.source}
              {metadata.provider ? ` / ${metadata.provider}` : ""} ↗
            </a>
          ) : (
            <span>资料来源：{metadata.source}</span>
          )}{" "}
          ·{" "}
          {match.selection === "manual"
            ? "手动匹配"
            : `${metadata.source} 影片资料`}{" "}
          · 缓存有效至{" "}
          {new Date(metadata.expiresAt * 1000).toLocaleDateString()}
        </p>
      )}
    </div>
  );
}

type Status = {
  sources: SourceStatus[];
  revision: number;
  configured: boolean;
  running: boolean;
  error: string;
  cooldownUntil: number;
};
export function MetadataStatus() {
  const [status, setStatus] = useState<Status | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    let active = true;
    let timer: ReturnType<typeof setTimeout>;
    let revision = -1;
    async function refresh() {
      try {
        const result = await api<Status>("/api/metadata/status");
        if (!active) return;
        setStatus(result);
        if (!result.running && revision !== result.revision)
          window.dispatchEvent(new Event("metadata-updated"));
        if (!result.running) revision = result.revision;
      } catch (error) {
        if (active) setError(message(error));
      }
      if (active) timer = setTimeout(refresh, 1500);
    }
    void refresh();
    return () => {
      active = false;
      clearTimeout(timer);
    };
  }, []);
  async function refresh() {
    setBusy(true);
    setError("");
    try {
      await api("/api/metadata/refresh", {});
      setStatus((s) => s && { ...s, running: true });
    } catch (error) {
      setError(message(error));
    } finally {
      setBusy(false);
    }
  }
  return (
    <section className="metadata-status" aria-label="影片资料源设置与状态">
      <div>
        <strong>影片资料</strong>
        <span className="fine">
          {!status
            ? "正在读取设置…"
            : !status.configured
              ? "未配置影片资料源（TMDB / TVDB / MetaTube）；本地浏览与播放不受影响。请在影片资料源配置页面设置服务。"
              : status.running
                ? "正在按目录优先级获取资料和海报，本地播放不受影响…"
                : "按目录选择资料源 · 扫描与匹配相互独立"}
        </span>
        <p className="fine">
          {status?.sources
            .map(
              (source) =>
                `${source.name}：${source.configured ? (source.cooldownUntil * 1000 > Date.now() ? "冷却中" : "已配置") : "未配置"}`,
            )
            .join(" · ")}{" "}
          · <a href="/settings/metadata">配置影片资料源</a>
          {" · "}
          <a href="/directories">配置目录资料源</a>
        </p>
        {(error || status?.error) && (
          <p className="notice" role="status">
            {error || status?.error}
          </p>
        )}
      </div>
      {status?.configured && (
        <button
          className="secondary"
          disabled={busy || status.running}
          onClick={refresh}
        >
          匹配 / 刷新到期资料
        </button>
      )}
    </section>
  );
}

export function TMDBCredits() {
  return (
    <section className="tmdb-credits" aria-label="关于与资料署名">
      <a href="https://www.themoviedb.org" target="_blank" rel="noreferrer">
        <img src="/assets/tmdb-logo.svg" alt="TMDB" width="72" />
      </a>
      <span>
        This product uses the TMDB API but is not endorsed or certified by TMDB.
      </span>
      <a href="https://www.thetvdb.com" target="_blank" rel="noreferrer">
        Metadata provided by TheTVDB ↗
      </a>
    </section>
  );
}
