import { useEffect, useRef, useState } from "react";
import { api, message } from "./api";
import { PlayerLogo } from "./player-logos";

export function ExternalPlayer({
  id,
  compact = false,
}: {
  id: string;
  compact?: boolean;
}) {
  const [url, setURL] = useState("");
  const [error, setError] = useState("");
  const [feedback, setFeedback] = useState("");
  const [busy, setBusy] = useState(true);
  const [attempt, setAttempt] = useState(0);
  const [detailsOpen, setDetailsOpen] = useState(false);
  const [manualCopy, setManualCopy] = useState(false);
  const address = useRef<HTMLInputElement>(null);

  useEffect(() => {
    const controller = new AbortController();
    setBusy(true);
    setError("");
    setURL("");
    // Resolve once, ahead of the click: app links and clipboard calls retain
    // the user's activation instead of waiting for a network request first.
    void api<{ path: string }>(
      `/api/media/${encodeURIComponent(id)}/stream-link`,
      {},
      { signal: controller.signal },
    )
      .then((result) => {
        if (!controller.signal.aborted)
          setURL(new URL(result.path, window.location.origin).href);
      })
      .catch((error) => {
        if (!controller.signal.aborted) setError(message(error));
      })
      .finally(() => {
        if (!controller.signal.aborted) setBusy(false);
      });
    return () => controller.abort();
  }, [id, attempt]);
  useEffect(() => {
    if (detailsOpen && manualCopy) {
      address.current?.focus();
      address.current?.select();
    }
  }, [detailsOpen, manualCopy]);
  async function copy() {
    if (!url) return;
    setDetailsOpen(true);
    setManualCopy(false);
    try {
      await navigator.clipboard.writeText(url);
      setFeedback("已复制串流地址");
    } catch {
      setManualCopy(true);
      setFeedback("剪贴板不可用，请复制下方已选中的完整地址。");
    }
  }
  const local = ["localhost", "127.0.0.1", "[::1]"].includes(
    window.location.hostname,
  );
  const players = [
    {
      logo: "pot",
      label: "Pot",
      name: "PotPlayer",
      href: `potplayer://${url}`,
    },
    { logo: "vlc", label: "VLC", name: "VLC", href: `vlc://${url}` },
    {
      logo: "iina",
      label: "IINA",
      name: "IINA",
      href: `iina://open?url=${encodeURIComponent(url)}`,
    },
  ] as const;
  return (
    <section
      className={`external-player${compact ? " external-player-compact" : ""}`}
      aria-label="外部播放器"
      aria-busy={busy}
    >
      {!compact && <h2>使用外部播放器</h2>}
      <div className="external-apps">
        {players.map((player) => (
          <a
            key={player.logo}
            className="external-app-button"
            href={url ? player.href : undefined}
            aria-label={`用 ${player.name} 打开`}
            title={`用 ${player.name} 打开`}
            aria-disabled={!url}
            tabIndex={url ? undefined : -1}
            onClick={(event) => {
              if (!url) {
                event.preventDefault();
                return;
              }
              setDetailsOpen(true);
              setFeedback(
                `已请求用 ${player.name} 打开；若未唤起，请复制链接，在播放器中打开 URL。`,
              );
            }}
          >
            <PlayerLogo name={player.logo} />
            <span>{player.label}</span>
          </a>
        ))}
      </div>
      <button
        className="external-app-button external-copy"
        disabled={!url}
        onClick={() => void copy()}
      >
        <PlayerLogo name="copy" />
        <span>复制链接</span>
      </button>
      {busy && (
        <p className="fine" role="status">
          正在准备串流地址…
        </p>
      )}
      {error && (
        <div className="external-error">
          <p role="alert" className="error">
            串流不可用：{error}
          </p>
          <button
            className="secondary"
            onClick={() => setAttempt((value) => value + 1)}
          >
            重试串流地址
          </button>
        </div>
      )}
      <details
        className="external-help"
        open={detailsOpen}
        onToggle={(event) => setDetailsOpen(event.currentTarget.open)}
      >
        <summary>串流说明与地址</summary>
        <p className="fine">
          外部播放不会更新网页观看进度，外挂字幕需自行加载。
        </p>
        <p className="notice">
          持有链接者可读取此视频，链接不会随时间自动失效；仅用于可信局域网，不要公开分享。
        </p>
        {local && (
          <p className="notice">
            当前地址仅供本机使用。若在其他设备播放，请使用服务器局域网 IP
            或域名访问网页后重新获取链接。
          </p>
        )}
        <p className="fine">
          需要在观看设备上安装对应播放器。浏览器可能询问是否打开应用；VLC、PotPlayer
          并非所有安装方式都注册网页协议，IINA 需要
          1.4.4+。网页不能确认是否唤起成功；没有反应时请复制链接，在播放器中“打开
          URL”。
        </p>
        {url && (
          <>
            <label htmlFor={`stream-${id}`}>串流地址（可手动复制）</label>
            <input
              ref={address}
              id={`stream-${id}`}
              className="stream-url"
              readOnly
              value={url}
              onFocus={(event) => event.currentTarget.select()}
            />
          </>
        )}
      </details>
      {feedback && (
        <p role="status" className="fine external-feedback">
          {feedback}
        </p>
      )}
    </section>
  );
}
