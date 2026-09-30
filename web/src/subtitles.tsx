import { useEffect, useState, type RefObject } from "react";
import { api, message } from "./api";

type Subtitle = { id: string; filename: string; format: string };
export function Subtitles({
  id,
  video,
}: {
  id: string;
  video: RefObject<HTMLVideoElement | null>;
}) {
  const [items, setItems] = useState<Subtitle[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [selected, setSelected] = useState("");
  const [error, setError] = useState("");
  const [status, setStatus] = useState("");
  const base = `/api/media/${encodeURIComponent(id)}/subtitles`;
  useEffect(() => {
    let active = true;
    api<{ items: Subtitle[] }>(base)
      .then((result) => {
        if (active) {
          setItems(result.items);
          setLoaded(true);
        }
      })
      .catch((error) => {
        if (active) {
          setError(message(error));
          setLoaded(true);
        }
      });
    return () => {
      active = false;
    };
  }, [base]);
  useEffect(() => {
    const element = video.current;
    setError("");
    if (!element || !selected) {
      setStatus("字幕已关闭");
      return;
    }
    const controller = new AbortController();
    let track: HTMLTrackElement | undefined, url: string | undefined;
    setStatus("正在加载字幕…");
    async function load() {
      try {
        const response = await fetch(
          `${base}/${encodeURIComponent(selected)}`,
          { credentials: "same-origin", signal: controller.signal },
        );
        if (!response.ok) {
          if (response.status === 401) window.location.replace("/login");
          const result = await response.json();
          throw new Error(result.error || "字幕读取失败");
        }
        const text = await response.text();
        if (controller.signal.aborted) return;
        url = URL.createObjectURL(new Blob([text], { type: "text/vtt" }));
        track = document.createElement("track");
        track.kind = "subtitles";
        track.label =
          items.find((item) => item.id === selected)?.filename || "外挂字幕";
        track.src = url;
        track.default = true;
        track.onload = () => {
          if (!track?.track.cues?.length) {
            setError("浏览器未解析出有效字幕，视频仍可播放。");
            setStatus("");
            return;
          }
          track.track.mode = "showing";
          setStatus(`字幕已开启：${track.label}`);
        };
        track.onerror = () => {
          setError("浏览器无法加载此字幕，视频仍可播放。");
          setStatus("");
        };
        element!.append(track);
        track.track.mode = "showing";
      } catch (error) {
        if (!controller.signal.aborted) {
          setError(message(error));
          setStatus("");
        }
      }
    }
    void load();
    return () => {
      controller.abort();
      if (track) {
        track.onload = null;
        track.onerror = null;
        track.track.mode = "disabled";
        track.remove();
      }
      if (url) URL.revokeObjectURL(url);
    };
  }, [base, selected, video, items]);
  return (
    <div className="subtitle-controls">
      <label htmlFor={`subtitles-${id}`}>外挂字幕</label>
      <select
        id={`subtitles-${id}`}
        value={selected}
        onChange={(event) => setSelected(event.target.value)}
        disabled={!loaded || items.length === 0}
      >
        <option value="">关闭字幕</option>
        {items.map((item) => (
          <option key={item.id} value={item.id}>
            {item.filename} · {item.format.toUpperCase()}
          </option>
        ))}
      </select>
      {loaded && items.length === 0 && !error && (
        <p className="fine">未发现同目录匹配的 UTF-8 SRT/VTT 外挂字幕。</p>
      )}
      {status && (
        <p role="status" className="fine">
          {status}
        </p>
      )}
      {error && (
        <p role="alert" className="error">
          {error}
        </p>
      )}
    </div>
  );
}
