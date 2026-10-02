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
              else setFeedback(`已请求用 ${name} 打开。`);
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
              setManual(false);
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
      {query.isPending && <p role="status">正在准备播放器…</p>}
      {manual && url && (
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
