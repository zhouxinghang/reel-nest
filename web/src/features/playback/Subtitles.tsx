import { useEffect, useRef, useState, type RefObject } from "react";
import { useQuery } from "@tanstack/react-query";
import { APIError } from "../../shared/api";
import { useAPI } from "../../shared/services";
import { QueryError, ui } from "../../shared/ui";
export function Subtitles({
  id,
  video,
}: {
  id: string;
  video: RefObject<HTMLVideoElement | null>;
}) {
  const api = useAPI();
  const [cursor, setCursor] = useState("");
  const [selected, setSelected] = useState("");
  const track = useRef<HTMLTrackElement | null>(null);
  const list = useQuery({
    queryKey: ["subtitles", id, cursor],
    gcTime: 0,
    queryFn: ({ signal }) =>
      api("get", "/api/media/{id}/subtitles", {
        path: { id },
        query: { cursor, limit: 50 },
        signal,
      }),
  });
  const content = useQuery({
    queryKey: ["subtitle-content", id, selected],
    enabled: !!selected,
    gcTime: 0,
    retry: false,
    queryFn: ({ signal }) =>
      api("get", "/api/media/{id}/subtitles/{subtitle}", {
        path: { id, subtitle: selected },
        signal,
      }),
  });
  useEffect(() => {
    if (list.error instanceof APIError && list.error.code === "CURSOR_STALE") {
      setCursor("");
      setSelected("");
    }
  }, [list.error]);
  useEffect(() => {
    const element = video.current;
    if (!content.data || !selected || !element) return;
    const url = URL.createObjectURL(
      new Blob([content.data], { type: "text/vtt" }),
    );
    const node = document.createElement("track");
    track.current = node;
    node.kind = "subtitles";
    node.label = "外挂字幕";
    node.srclang = "zh";
    node.src = url;
    node.default = true;
    const show = () => {
      node.track.mode = "showing";
    };
    node.addEventListener("load", show);
    element.append(node);
    node.track.mode = "showing";
    return () => {
      node.removeEventListener("load", show);
      node.track.mode = "disabled";
      node.remove();
      track.current = null;
      URL.revokeObjectURL(url);
    };
  }, [content.data, selected, video]);
  return (
    <section aria-label="外挂字幕">
      <label>
        外挂字幕
        <select
          className={ui.input}
          value={selected}
          onChange={(e) => setSelected(e.target.value)}
        >
          <option value="">关闭字幕</option>
          {list.data?.items.map((s) => (
            <option key={s.id} value={s.id}>
              {s.filename}
            </option>
          ))}
        </select>
      </label>
      <QueryError error={list.error || content.error} />
      {content.isFetching && <p role="status">正在读取字幕…</p>}
      {cursor && (
        <button
          className={ui.button}
          onClick={() => {
            setSelected("");
            setCursor("");
          }}
        >
          字幕首页
        </button>
      )}
      {list.data?.page.nextCursor && (
        <button
          className={ui.button}
          onClick={() => {
            setSelected("");
            setCursor(list.data!.page.nextCursor!);
          }}
        >
          下一页字幕
        </button>
      )}
    </section>
  );
}
