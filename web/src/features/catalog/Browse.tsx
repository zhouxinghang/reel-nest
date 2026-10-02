import {
  useEffect,
  useRef,
  useCallback,
  useState,
  type FormEvent,
} from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useSearchParams } from "react-router-dom";
import type { Schema } from "../../shared/api";
import { APIError } from "../../shared/api";
import { useAPI } from "../../shared/services";
import {
  Empty,
  Loading,
  Pagination,
  QueryError,
  ui,
  useStaleCursor,
} from "../../shared/ui";
import { queries, type CatalogParams } from "./queries";
import { catalogState, categories, changeFilters, labels } from "./url";
import { Cards } from "./Card";
import styles from "./catalog.module.css";

function Entries({
  params,
  layout,
  cursorKey = "cursor",
  onStale,
}: {
  params: CatalogParams;
  layout: "grid" | "list";
  cursorKey?: string;
  onStale?: () => void;
}) {
  const query = useQuery(queries.catalog(useAPI(), params));
  const stale =
    query.error instanceof APIError && query.error.code === "CURSOR_STALE";
  useStaleCursor(onStale ? null : query.error, cursorKey);
  useEffect(() => {
    if (stale && onStale) onStale();
  }, [stale, onStale]);
  return (
    <>
      <QueryError
        error={stale ? null : query.error}
        retry={() => void query.refetch()}
      />
      {query.isPending && <Loading />}
      {query.data && (
        <>
          {query.data.items.length ? (
            <Cards items={query.data.items} layout={layout} />
          ) : (
            <Empty />
          )}
          <Pagination page={query.data.page} cursorKey={cursorKey} />
        </>
      )}
    </>
  );
}
function Group({
  group,
  params,
  layout,
  refresh,
}: {
  group: Schema["Group"];
  params: CatalogParams;
  layout: "grid" | "list";
  refresh: () => void;
}) {
  const [search] = useSearchParams();
  const cursorKey = `group.${group.library.id}`;
  return (
    <section className={styles.group} aria-label={group.library.name}>
      <h2>
        <Link to={`?${changeFilters(search, { library: group.library.id })}`}>
          {group.library.name}
        </Link>{" "}
        <small className={ui.muted}>({group.total})</small>
      </h2>
      <Entries
        params={{
          ...params,
          libraryId: group.library.id,
          cursor: search.get(cursorKey) || group.entriesCursor || undefined,
        }}
        layout={layout}
        cursorKey={cursorKey}
        onStale={refresh}
      />
    </section>
  );
}
function Groups({
  params,
  layout,
}: {
  params: CatalogParams;
  layout: "grid" | "list";
}) {
  const api = useAPI();
  const client = useQueryClient();
  const [, setSearch] = useSearchParams();
  const query = useQuery(queries.groups(api, params));
  useStaleCursor(query.error);
  // A stale child first-page token must refresh the parent snapshot, not repeat
  // the same expired entriesCursor. Remove children until the new group page arrives.
  const [recovering, setRecovering] = useState(false);
  const recovery = useRef(false);
  const refresh = useCallback(() => {
    if (recovery.current) return;
    recovery.current = true;
    setRecovering(true);
    setSearch((current) => changeFilters(current, {}), { replace: true });
    void client.invalidateQueries({ queryKey: ["groups"] }).finally(() => {
      recovery.current = false;
      setRecovering(false);
    });
  }, [client, setSearch]);
  if (recovering) return <Loading />;
  return (
    <>
      <QueryError error={query.error} retry={() => void query.refetch()} />
      {query.isPending && <Loading />}
      {query.data && !query.isError && (
        <>
          {!query.data.items.length && <Empty />}
          {query.data.items.map((group) => (
            <Group
              key={group.library.id}
              group={group}
              params={params}
              layout={layout}
              refresh={refresh}
            />
          ))}
          <Pagination page={query.data.page} />
        </>
      )}
    </>
  );
}
export function Browse() {
  const [search, setSearch] = useSearchParams();
  const { category, params, layout } = catalogState(search);
  const [draft, setDraft] = useState(params.q || "");
  useEffect(() => setDraft(params.q || ""), [params.q]);
  function submit(event: FormEvent) {
    event.preventDefault();
    setSearch(changeFilters(search, { q: draft.trim() }));
  }
  const grouped =
    !params.libraryId && params.view !== "recent" && params.view !== "continue";
  return (
    <>
      <h1>{labels[category]}</h1>
      <form className={styles.filters} onSubmit={submit}>
        <label>
          搜索
          <input
            className={ui.input}
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            maxLength={200}
            placeholder="搜索文件名或标题"
          />
        </label>
        <button className={ui.button}>搜索</button>
        <label>
          分类
          <select
            className={ui.input}
            aria-label="分类"
            value={category}
            onChange={(e) =>
              setSearch(changeFilters(search, { category: e.target.value }))
            }
          >
            {categories.map((c) => (
              <option key={c} value={c}>
                {labels[c]}
              </option>
            ))}
          </select>
        </label>
        <label>
          排序
          <select
            className={ui.input}
            aria-label="排序"
            value={params.sort}
            disabled={params.view !== "browse" && params.view !== "unmatched"}
            onChange={(e) =>
              setSearch(changeFilters(search, { sort: e.target.value }))
            }
          >
            <option value="title">标题</option>
            <option value="recent">最近添加</option>
            {params.sort === "favorite" && (
              <option value="favorite">收藏时间</option>
            )}
            {params.sort === "progress" && (
              <option value="progress">观看时间</option>
            )}
          </select>
        </label>
        <label>
          视图
          <select
            className={ui.input}
            aria-label="视图"
            value={layout}
            onChange={(e) => {
              const next = new URLSearchParams(search);
              next.set("layout", e.target.value);
              setSearch(next);
            }}
          >
            <option value="grid">海报</option>
            <option value="list">列表</option>
          </select>
        </label>
      </form>
      {params.libraryId && (
        <p className={ui.muted}>
          当前媒体库范围 ·{" "}
          <Link to={`?${changeFilters(search, { library: "" })}`}>
            查看全部媒体库
          </Link>
        </p>
      )}
      {grouped ? (
        <Groups params={{ ...params, limit: 10 }} layout={layout} />
      ) : (
        <Entries params={params} layout={layout} />
      )}
    </>
  );
}
