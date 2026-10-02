import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useAPI } from "../../shared/services";
import { QueryError, ui } from "../../shared/ui";
import styles from "./playback.module.css";
export function playerLinks(url: string) {
  return {
    IINA: `iina://open?url=${encodeURIComponent(url)}`,
    PotPlayer: `potplayer://${url}`,
    VLC: `vlc://${url}`,
  };
}
export function External({ id }: { id: string }) {
  const api = useAPI();
  const input = useRef<HTMLInputElement>(null);
  const [feedback, setFeedback] = useState("");
  const [manual, setManual] = useState(false);
  const query = useQuery({
    queryKey: ["stream-link", id],
    gcTime: 0,
    retry: false,
    staleTime: Infinity,
    queryFn: ({ signal }) =>
      api("post", "/api/media/{id}/stream-link", {
        path: { id },
        body: {},
        signal,
      }),
  });
  const url = query.data
    ? new URL(query.data.path, window.location.origin).href
    : "";
  useEffect(() => {
    if (manual) {
      input.current?.focus();
      input.current?.select();
    }
  }, [manual]);
  return (
    <section className={styles.external} aria-label="外部播放器">
      <h2>外部播放器</h2>
      <div className={styles.actions}>
        {Object.entries(playerLinks(url)).map(([name, href]) => (
          <a
            key={name}
            className={ui.button}
            href={url ? href : undefined}
            aria-disabled={!url}
            onClick={(e) => {
              if (!url) e.preventDefault();
              else
                setFeedback(
                  `已请求用 ${name} 打开；网页无法确认唤起或播放成功，没有反应请复制地址。`,
                );
            }}
          >
            用 {name} 打开
          </a>
        ))}
        <button
          className={ui.button}
          disabled={!url}
          onClick={async () => {
            try {
              await navigator.clipboard.writeText(url);
              setFeedback("已复制串流地址");
            } catch {
              setManual(true);
              input.current?.focus();
              input.current?.select();
              setFeedback("剪贴板不可用，请手动复制完整地址。");
            }
          }}
        >
          复制串流地址
        </button>
      </div>
      <QueryError error={query.error} retry={() => void query.refetch()} />
      {query.isPending && <p role="status">正在准备串流地址…</p>}
      <p>
        链接不过期，持有者可只读此视频；仅用于可信局域网，不要公开分享。外部播放不会更新网页进度，外挂字幕需自行加载。
      </p>
      <p>
        需要在观看设备安装播放器并注册协议（IINA 1.4.4+）。VLC / PotPlayer
        的安装方式可能不支持网页唤起，可在应用内“打开 URL”。
      </p>
      {["localhost", "127.0.0.1", "[::1]"].includes(location.hostname) && (
        <p>
          当前地址仅供本机使用；跨设备请通过服务器局域网 IP
          或域名访问后获取链接。
        </p>
      )}
      {url && (
        <label>
          串流地址（可手动复制）
          <input
            ref={input}
            className={ui.input}
            readOnly
            value={url}
            onFocus={(e) => e.currentTarget.select()}
          />
        </label>
      )}
      {feedback && <p role="status">{feedback}</p>}
    </section>
  );
}
