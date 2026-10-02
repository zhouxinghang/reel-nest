import { useState } from "react";
import { api, message } from "./api";

export type MetadataSource = "tmdb" | "tvdb" | "metatube";
export type SourceStatus = {
  id: MetadataSource;
  name: string;
  configured: boolean;
  cooldownUntil: number;
};
const sourceNames = {
  tmdb: "TheMovieDB (TMDB)",
  tvdb: "TheTVDB (TVDB)",
  metatube: "MetaTube（仅电影）",
};

export function SourceOrder({
  value,
  onChange,
  statuses,
  disabled = false,
}: {
  value: MetadataSource[];
  onChange: (value: MetadataSource[]) => void;
  statuses: SourceStatus[];
  disabled?: boolean;
}) {
  const order = [
    ...value,
    ...(["tmdb", "tvdb", "metatube"] as const).filter(
      (id) => !value.includes(id),
    ),
  ];
  function move(index: number, direction: number) {
    const next = [...value];
    [next[index], next[index + direction]] = [
      next[index + direction],
      next[index],
    ];
    onChange(next);
  }
  return (
    <fieldset className="source-order" disabled={disabled}>
      <legend>影片资料源优先级</legend>
      <p className="fine">
        从上到下获取影片资料和海报；未配置、失败或无可靠匹配时尝试下一源。全部取消则暂停自动获取，不删除已缓存资料。
      </p>
      <ol>
        {order.map((id) => {
          const index = value.indexOf(id);
          const configured = statuses.find(
            (source) => source.id === id,
          )?.configured;
          return (
            <li key={id}>
              <label>
                <input
                  type="checkbox"
                  checked={index >= 0}
                  onChange={(event) =>
                    onChange(
                      event.target.checked
                        ? [...value, id]
                        : value.filter((source) => source !== id),
                    )
                  }
                />
                <span>
                  {index >= 0 ? `${index + 1}. ` : ""}
                  {sourceNames[id]}
                  <small className="fine">
                    {configured ? "服务已配置" : "未配置服务，将跳过"}
                  </small>
                </span>
              </label>
              <div className="source-order-actions">
                <button
                  type="button"
                  className="secondary"
                  aria-label={`上移 ${sourceNames[id]}`}
                  disabled={index <= 0}
                  onClick={() => move(index, -1)}
                >
                  ↑
                </button>
                <button
                  type="button"
                  className="secondary"
                  aria-label={`下移 ${sourceNames[id]}`}
                  disabled={index < 0 || index >= value.length - 1}
                  onClick={() => move(index, 1)}
                >
                  ↓
                </button>
              </div>
            </li>
          );
        })}
      </ol>
    </fieldset>
  );
}

export function DirectorySources({
  id,
  sources,
  statuses,
  saved,
}: {
  id: string;
  sources: MetadataSource[];
  statuses: SourceStatus[];
  saved: () => Promise<void>;
}) {
  const [value, setValue] = useState(sources);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const changed = JSON.stringify(value) !== JSON.stringify(sources);
  return (
    <form
      className="directory-sources"
      onSubmit={async (event) => {
        event.preventDefault();
        setBusy(true);
        setError("");
        try {
          await api(`/api/directories/${id}/metadata-sources`, {
            metadataSources: value,
          });
          await saved();
          window.dispatchEvent(new Event("metadata-updated"));
        } catch (error) {
          setError(message(error));
        } finally {
          setBusy(false);
        }
      }}
    >
      <SourceOrder
        value={value}
        onChange={setValue}
        statuses={statuses}
        disabled={busy}
      />
      <p className="fine">
        保存后按新顺序重新评估自动匹配；手动确认的作品不会被覆盖。
      </p>
      <button className="secondary" disabled={busy || !changed}>
        {busy ? "正在保存…" : "保存资料源"}
      </button>
      {error && (
        <p className="error" role="alert">
          {error}
        </p>
      )}
    </form>
  );
}
