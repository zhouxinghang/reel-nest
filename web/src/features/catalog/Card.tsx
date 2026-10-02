import {
  useIsMutating,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query";
import { Link } from "react-router-dom";
import type { Schema } from "../../shared/api";
import { useAPI } from "../../shared/services";
import { QueryError } from "../../shared/ui";
import { confirmFavorite, favoriteAffected } from "./cache";
import styles from "./catalog.module.css";

export function Favorite({ card }: { card: Schema["Card"] }) {
  const api = useAPI();
  const client = useQueryClient();
  const mutationKey = ["favorite", card.entryType, card.id];
  const busy = useIsMutating({ mutationKey }) > 0;
  const mutation = useMutation({
    mutationKey,
    mutationFn: (favorite: boolean) =>
      api("put", "/api/favorites/{target}/{id}", {
        path: { target: card.entryType, id: card.id },
        body: { favorite },
      }),
    onMutate: () =>
      client.cancelQueries({ predicate: (q) => favoriteAffected(q, card) }),
    onError: () =>
      client.invalidateQueries({ predicate: (q) => favoriteAffected(q, card) }),
    onSuccess: (favorite) => confirmFavorite(client, card, favorite),
  });
  return (
    <div className={styles.favorite}>
      <button
        aria-label={`${card.favorite.favorite ? "取消收藏" : "收藏"} ${card.title}`}
        aria-pressed={card.favorite.favorite}
        disabled={busy}
        onClick={() => {
          if (client.isMutating({ mutationKey }) === 0)
            mutation.mutate(!card.favorite.favorite);
        }}
      >
        {card.favorite.favorite ? "♥" : "♡"}
      </button>
      <QueryError error={mutation.error} />
    </div>
  );
}
export function Card({ card }: { card: Schema["Card"] }) {
  const artwork = card.metadata?.artwork.primary;
  return (
    <article className={styles.card} data-testid="media-card">
      <Link
        className={styles.cardLink}
        to={`/${card.entryType}/${encodeURIComponent(card.id)}`}
      >
        <div className={styles.poster}>
          {artwork ? (
            <img
              src={`/api/assets/${encodeURIComponent(artwork)}/content`}
              alt=""
              loading="lazy"
              onError={(event) => {
                event.currentTarget.hidden = true;
              }}
            />
          ) : (
            <span aria-hidden="true">
              {card.entryType === "series" ? "▤" : "▷"}
            </span>
          )}
          {card.entryType === "series" && (
            <span className={styles.badge}>{card.episodeCount} 集</span>
          )}
          {card.entryType === "media" && card.progress && (
            <progress
              aria-label="观看进度"
              max={card.progress.duration || 1}
              value={card.progress.position}
            />
          )}
        </div>
        <h3>{card.title}</h3>
        <p>
          {card.metadata?.year ?? (card.kind === "movie" ? "电影" : "电视剧")}
          {!card.metadata && " · 未匹配"}
        </p>
      </Link>
      <Favorite card={card} />
    </article>
  );
}
export function Cards({
  items,
  layout = "grid",
}: {
  items: Schema["Card"][];
  layout?: "grid" | "list" | "shelf";
}) {
  return (
    <div className={styles.cards} data-layout={layout}>
      {items.map((card) => (
        <Card key={card.entryKey} card={card} />
      ))}
    </div>
  );
}
