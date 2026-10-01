import { useEffect, useRef, useState, type FormEvent } from "react";
import { api, message } from "./api";
import type { TitleMetadata } from "./media";
import { Icon } from "./icons";

type Target = { kind: "movie" | "series"; id: string; title: string };
export function Matching({
  target,
  compact = false,
}: {
  target: Target;
  compact?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const [saved, setSaved] = useState(false);
  const trigger = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    if (open) return () => trigger.current?.focus();
  }, [open]);
  return (
    <div className="matching-entry">
      <button
        ref={trigger}
        className={compact ? "detail-icon-action" : "secondary"}
        aria-label="手动匹配影片资料"
        title="手动匹配影片资料"
        onClick={() => {
          setOpen(true);
          setSaved(false);
        }}
      >
        {compact ? <Icon name="edit" /> : "手动匹配影片资料"}
      </button>
      {saved && (
        <p className="fine" role="status">
          已更新影片资料，源文件与观看进度保持不变。
        </p>
      )}
      {open && (
        <MatchingDialog
          target={target}
          close={() => setOpen(false)}
          saved={() => {
            setOpen(false);
            setSaved(true);
            window.dispatchEvent(new Event("metadata-updated"));
          }}
        />
      )}
    </div>
  );
}
function MatchingDialog({
  target,
  close,
  saved,
}: {
  target: Target;
  close: () => void;
  saved: () => void;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  const request = useRef<AbortController | null>(null);
  const [query, setQuery] = useState(target.title);
  const [items, setItems] = useState<TitleMetadata[] | null>(null);
  const [more, setMore] = useState(false);
  const [selected, setSelected] = useState<number | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState<"search" | "save" | null>(null);
  const base = `/api/matching/${target.kind}/${encodeURIComponent(target.id)}`;
  useEffect(() => {
    dialog.current?.showModal();
    return () => {
      request.current?.abort();
    };
  }, []);
  async function search(event: FormEvent) {
    event.preventDefault();
    request.current?.abort();
    const controller = new AbortController();
    request.current = controller;
    setBusy("search");
    setError("");
    setSelected(null);
    setItems(null);
    try {
      const result = await api<{
        items: TitleMetadata[];
        moreResults: boolean;
      }>(`${base}/search?q=${encodeURIComponent(query.trim())}`, undefined, {
        signal: controller.signal,
      });
      if (!controller.signal.aborted) {
        setItems(result.items);
        setMore(result.moreResults);
      }
    } catch (error) {
      if (!controller.signal.aborted) setError(message(error));
    } finally {
      if (!controller.signal.aborted) setBusy(null);
    }
  }
  async function confirm() {
    if (selected === null) return;
    setBusy("save");
    setError("");
    const controller = new AbortController();
    request.current = controller;
    try {
      await api(
        `${base}/selection`,
        { tmdbId: selected, kind: target.kind },
        { signal: controller.signal },
      );
      saved();
    } catch (error) {
      if (!controller.signal.aborted) setError(message(error));
    } finally {
      if (!controller.signal.aborted) setBusy(null);
    }
  }
  return (
    <dialog
      ref={dialog}
      className="matching-dialog"
      aria-labelledby="matching-title"
      onCancel={(event) => {
        event.preventDefault();
        if (busy !== "save") close();
      }}
    >
      <h2 id="matching-title">手动匹配影片资料</h2>
      <p className="muted">
        {target.kind === "movie" ? "电影" : "电视剧"} · {target.title}
      </p>
      <p className="fine">
        仅修改影片资料，不改名、移动或改写视频，也不改变季集关系和观看进度。电视剧的已组织单集共享同一资料关联。
      </p>
      <form className="matching-search" onSubmit={search}>
        <label htmlFor="matching-query">搜索作品标题</label>
        <div>
          <input
            id="matching-query"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            required
            maxLength={200}
            disabled={busy !== null}
          />
          <button
            className="secondary"
            disabled={busy !== null || !query.trim()}
          >
            {busy === "search" ? "正在搜索…" : "搜索 TMDB"}
          </button>
        </div>
      </form>
      {error && (
        <p className="error" role="alert">
          {error}。原有资料关联未改变。
        </p>
      )}
      {items?.length === 0 && (
        <p role="status" className="notice">
          没有找到候选，请换个标题重试。原有资料关联未改变。
        </p>
      )}
      {more && items && (
        <p className="notice">仅显示前 20 个候选，请细化标题以缩小范围。</p>
      )}
      {items && items.length > 0 && (
        <fieldset className="matching-candidates" disabled={busy !== null}>
          <legend>选择正确作品，确认后才更新资料</legend>
          {items.map((item) => (
            <label className="matching-candidate" key={item.tmdbId}>
              <input
                type="radio"
                name="tmdb-candidate"
                checked={selected === item.tmdbId}
                onChange={() => setSelected(item.tmdbId)}
              />
              <span>
                <strong>{item.title}</strong>
                <span className="fine">
                  {item.originalTitle} ·{" "}
                  {item.kind === "movie" ? "电影" : "电视剧"} ·{" "}
                  {item.year || "年份未知"} · TMDB #{item.tmdbId}
                </span>
              </span>
            </label>
          ))}
        </fieldset>
      )}
      <div className="matching-actions">
        <button
          className="secondary"
          disabled={busy === "save"}
          onClick={close}
        >
          取消
        </button>
        <button
          className="primary"
          disabled={selected === null || busy !== null}
          onClick={confirm}
        >
          {busy === "save" ? "正在确认…" : "确认匹配"}
        </button>
      </div>
    </dialog>
  );
}
