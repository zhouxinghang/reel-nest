import { useQuery } from "@tanstack/react-query";
import { Link, useSearchParams } from "react-router-dom";
import { useAPI } from "../../shared/services";
import {
  Empty,
  Loading,
  Pagination,
  QueryError,
  ui,
  useStaleCursor,
} from "../../shared/ui";
import { queries } from "./queries";
import { Cards } from "./Card";
import styles from "./catalog.module.css";

export function Home() {
  const [search] = useSearchParams();
  const query = useQuery(queries.home(useAPI(), search.get("cursor") || ""));
  useStaleCursor(query.error);
  return (
    <>
      <header className={styles.heading}>
        <p className={ui.muted}>YOUR PERSONAL CINEMA</p>
        <h1>好故事，在家一起看。</h1>
      </header>
      <QueryError error={query.error} retry={() => void query.refetch()} />
      {query.isPending && <Loading />}
      {query.data && (
        <>
          {!!query.data.continueWatching.length && (
            <section aria-label="继续观看">
              <h2>
                <Link to="/library?category=continue">继续观看 →</Link>
              </h2>
              <Cards layout="shelf" items={query.data.continueWatching} />
            </section>
          )}
          <section aria-label="我的媒体库">
            <h2>我的媒体库</h2>
            {!query.data.libraries.length && <Empty />}
            {query.data.libraries.map(({ library, items }) => (
              <section
                key={library.id}
                className={styles.group}
                aria-label={library.name}
              >
                <h3>
                  <Link
                    to={`/library?library=${encodeURIComponent(library.id)}`}
                  >
                    {library.name} →
                  </Link>
                </h3>
                {items.length ? (
                  <Cards layout="shelf" items={items} />
                ) : (
                  <Empty />
                )}
              </section>
            ))}
            <Pagination page={query.data.librariesPage} />
          </section>
          <section aria-label="最近添加">
            <h2>
              <Link to="/library?category=recent">最近添加 →</Link>
            </h2>
            {query.data.recent.length ? (
              <Cards layout="shelf" items={query.data.recent} />
            ) : (
              <Empty />
            )}
          </section>
        </>
      )}
    </>
  );
}
