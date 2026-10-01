import { useState } from "react";
import { api, message } from "./api";
import { Icon } from "./icons";

export type FavoriteState = { favorite: boolean; favoritedAt: number };

export function FavoriteButton({
  kind,
  id,
  value,
  title,
  onChange,
}: {
  kind: "media" | "series";
  id: string;
  value: boolean;
  title: string;
  onChange: (state: FavoriteState) => void;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const label = value ? "取消收藏" : "添加收藏";
  async function toggle() {
    setBusy(true);
    setError("");
    try {
      const state = await api<FavoriteState>(
        `/api/favorites/${kind}/${encodeURIComponent(id)}`,
        { favorite: !value },
      );
      onChange(state);
    } catch (error) {
      setError(`收藏未更新：${message(error)}。请重试。`);
    } finally {
      setBusy(false);
    }
  }
  return (
    <span className="favorite-control">
      <button
        className={`detail-icon-action favorite-button${value ? " is-favorite" : ""}`}
        aria-label={`${label}：${title}`}
        aria-pressed={value}
        aria-busy={busy}
        title={label}
        disabled={busy}
        onClick={() => void toggle()}
      >
        <Icon name="heart" />
      </button>
      {error && (
        <span className="favorite-error error" role="alert">
          {error}
        </span>
      )}
    </span>
  );
}
