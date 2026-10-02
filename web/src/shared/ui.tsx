import { useEffect } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { APIError, errorMessage, type Schema } from "./api";
import { changeCursor } from "../features/catalog/url";
import styles from "./ui.module.css";

export { styles as ui };
export function QueryError({
  error,
  retry,
}: {
  error: unknown;
  retry?: () => void;
}) {
  const message = errorMessage(error);
  if (!message) return null;
  return (
    <div role="alert" className={styles.error}>
      <p>{message}</p>
      {retry && (
        <button type="button" className={styles.button} onClick={retry}>
          重试读取
        </button>
      )}
    </div>
  );
}
export function Loading() {
  return <p role="status">正在加载…</p>;
}
export function Empty() {
  return <p className={styles.muted}>没有符合条件的内容。</p>;
}
export function Pagination({
  page,
  cursorKey = "cursor",
  initial = null,
}: {
  page?: Schema["PageInfo"];
  cursorKey?: string;
  initial?: string | null;
}) {
  const [search] = useSearchParams();
  const current = search.get(cursorKey);
  return (
    <nav className={styles.pagination} aria-label="分页">
      {current && (
        <Link
          className={styles.button}
          to={`?${changeCursor(search, cursorKey, initial)}`}
        >
          回到首页
        </Link>
      )}
      {page?.nextCursor && (
        <Link
          className={styles.button}
          to={`?${changeCursor(search, cursorKey, page.nextCursor)}`}
        >
          下一页
        </Link>
      )}
    </nav>
  );
}
// Reset only the stale resource's URL cursor. Groups additionally clear child
// cursors, since each child is signed against the parent filter/revision.
export function useStaleCursor(
  error: unknown,
  cursorKey = "cursor",
  resetParent = false,
) {
  const [search, setSearch] = useSearchParams();
  const stale = error instanceof APIError && error.code === "CURSOR_STALE";
  useEffect(() => {
    if (!stale) return;
    if (resetParent) {
      setSearch((current) => changeCursor(current, "cursor", null), {
        replace: true,
      });
    } else if (search.has(cursorKey)) {
      setSearch((current) => changeCursor(current, cursorKey, null), {
        replace: true,
      });
    }
  }, [stale, cursorKey, resetParent, search, setSearch]);
  return stale;
}
