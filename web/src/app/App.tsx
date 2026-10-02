import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, Outlet, useLocation, useNavigate } from "react-router-dom";
import { useAPI } from "../shared/services";
import { Loading, QueryError, ui } from "../shared/ui";
import { keys } from "../features/catalog/queries";
import { categories, labels } from "../features/catalog/url";
import { TaskStatus } from "../features/tasks/TaskStatus";
import { ScanProvider, ScanSummary } from "../features/tasks/Scans";
import styles from "./app.module.css";

export function App() {
  const api = useAPI();
  const client = useQueryClient();
  const navigate = useNavigate();
  const location = useLocation();
  const session = useQuery({
    queryKey: keys.session,
    queryFn: ({ signal }) => api("get", "/api/session", { signal }),
    staleTime: Infinity,
  });
  const [open, setOpen] = useState(false);
  const menu = useRef<HTMLButtonElement>(null);
  const navigation = useRef<HTMLElement>(null);
  useEffect(() => setOpen(false), [location]);
  useEffect(() => {
    if (!open) return;
    navigation.current?.querySelector("a")?.focus();
    const close = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        setOpen(false);
        menu.current?.focus();
      }
    };
    document.addEventListener("keydown", close);
    return () => document.removeEventListener("keydown", close);
  }, [open]);
  const logout = useMutation({
    mutationFn: () => api("post", "/api/logout", { body: {} }),
    onSuccess: async () => {
      api.cancelAll();
      await client.cancelQueries();
      client.clear();
      await navigate("/login", { replace: true });
    },
  });
  if (!session.data)
    return (
      <main className={styles.loading}>
        <QueryError
          error={session.error}
          retry={() => void session.refetch()}
        />
        {session.isPending && <Loading />}
      </main>
    );
  const category =
    new URLSearchParams(location.search).get("category") || "all";
  return (
    <ScanProvider>
      <div className={styles.shell}>
        <a className={styles.skip} href="#main-content">
          跳到主要内容
        </a>
        <aside className={styles.sidebar} data-open={open}>
          <Link className={styles.brand} to="/">
            reel<span>nest</span>
          </Link>
          <p className={ui.muted}>我的媒体</p>
          <nav id="main-navigation" ref={navigation} aria-label="主导航">
            <Link
              to="/"
              aria-current={location.pathname === "/" ? "page" : undefined}
            >
              首页
            </Link>
            {categories.map((c) => (
              <Link
                key={c}
                to={`/library?category=${c}`}
                aria-current={
                  location.pathname === "/library" && category === c
                    ? "page"
                    : undefined
                }
              >
                {labels[c]}
              </Link>
            ))}
            <Link to="/libraries">媒体库管理</Link>
            <Link to="/tasks">影片资料任务</Link>
            <Link
              to="/settings/metadata"
              aria-current={
                location.pathname === "/settings/metadata" ? "page" : undefined
              }
            >
              影片资料源
            </Link>
          </nav>
        </aside>
        {open && (
          <button
            className={styles.scrim}
            aria-label="关闭导航"
            onClick={() => {
              setOpen(false);
              menu.current?.focus();
            }}
          />
        )}
        <div className={styles.body}>
          <header className={styles.header}>
            <button
              ref={menu}
              className={styles.menu}
              aria-label="打开导航"
              aria-expanded={open}
              aria-controls="main-navigation"
              onClick={() => setOpen(!open)}
            >
              ☰
            </button>
            <TaskStatus />
            <ScanSummary />
            <span className={ui.muted}>{session.data.username}</span>
            <button
              className={ui.button}
              disabled={logout.isPending}
              onClick={() => logout.mutate()}
            >
              退出
            </button>
          </header>
          <main id="main-content" tabIndex={-1} className={styles.content}>
            <QueryError error={logout.error} />
            <Outlet />
          </main>
        </div>
      </div>
    </ScanProvider>
  );
}
