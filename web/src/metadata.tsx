import { useEffect, useState, type ReactNode } from "react";
import { Icon } from "./icons";
import { api, message } from "./api";
import type { MetadataAssociation, TitleMetadata } from "./media";

export function Poster({
  metadata,
  title,
  detail = false,
  children,
}: {
  metadata?: TitleMetadata | null;
  title: string;
  detail?: boolean;
  children?: ReactNode;
}) {
  const [failed, setFailed] = useState("");
  return (
    <div
      className={`poster-placeholder${detail ? " detail-poster" : ""}`}
      data-tone={
        Array.from(title).reduce((sum, char) => sum + char.codePointAt(0)!, 0) %
        5
      }
    >
      {metadata?.posterUrl && failed !== metadata.posterUrl ? (
        <img
          className="poster-image"
          src={metadata.posterUrl}
          alt={`${title} 海报`}
          loading="lazy"
          onError={() => setFailed(metadata.posterUrl)}
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
            : "尚未关联影片资料，保留原始文件名供浏览。未匹配媒体也可以直接尝试播放，不要求 TMDB 凭据。")}
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
      {metadata && (
        <p className="fine">
          <a href={metadata.sourceUrl} target="_blank" rel="noreferrer">
            资料来源：TMDB ↗
          </a>{" "}
          · {match.selection === "manual" ? "手动匹配" : "TMDB 影片资料"} ·
          缓存有效至 {new Date(metadata.expiresAt * 1000).toLocaleDateString()}
        </p>
      )}
    </div>
  );
}

type Status = {
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
    <section className="metadata-status" aria-label="TMDB 设置与状态">
      <div>
        <strong>影片资料</strong>
        <span className="fine">
          {!status
            ? "正在读取设置…"
            : !status.configured
              ? "未配置 TMDB 凭据；本地浏览与播放不受影响。由部署者设置 TMDB_READ_ACCESS_TOKEN_FILE 后重启。"
              : status.running
                ? "正在后台获取 TMDB 资料，本地播放不受影响…"
                : "已配置 TMDB · 中文优先 · 扫描与匹配相互独立"}
        </span>
        {(error || status?.error) && (
          <p className="notice" role="status">
            {error || status?.error}
          </p>
        )}
      </div>
      {status?.configured && (
        <button
          className="secondary"
          disabled={
            busy || status.running || status.cooldownUntil * 1000 > Date.now()
          }
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
    </section>
  );
}
