import React, { useEffect, useState, type FormEvent } from "react";
import { createRoot } from "react-dom/client";
import { api, message } from "./api";
import "./style.css";
import "./detail.css";
import "./home.css";
import {
  Library,
  Directories,
  MediaDetails,
  readLibraryFilter,
  type LibraryFilter,
} from "./library";
import { SeriesDetails } from "./series";
import { PersonDetails } from "./people";
import { Icon } from "./icons";
import { MetadataSettings } from "./metadata-settings";

import { MetadataStatus, TMDBCredits } from "./metadata";

type Session = { username: string };

function Brand() {
  return (
    <a className="brand" href="/library">
      <span className="brand-mark" aria-hidden="true">
        <Icon name="play" />
      </span>
      <span>
        reel<span className="brand-accent">nest</span>
        <span className="brand-sub">你的私人家庭影院</span>
      </span>
    </a>
  );
}

function Login({ onLogin }: { onLogin: (session: Session) => void }) {
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    setBusy(true);
    setError("");
    try {
      const session = await api<Session>("/api/login", {
        username: form.get("username"),
        password: form.get("password"),
      });
      window.history.replaceState(null, "", "/library");
      onLogin(session);
    } catch (error) {
      setError(message(error));
    } finally {
      setBusy(false);
    }
  }
  return (
    <main className="login-shell">
      <section className="login-story">
        <Brand />
        <div className="story-copy">
          <p className="eyebrow">A LITTLE CLOSER TO HOME</p>
          <h1>
            好故事，
            <br />
            在家一起看。
          </h1>
          <p>
            把珍藏的电影与电视剧，
            <br />
            放进属于全家的放映室。
          </p>
          <div className="film-stack" aria-hidden="true">
            <div />
            <div />
            <div />
          </div>
        </div>
        <p className="fine">你的媒体 · 你的设备 · 一起观看</p>
      </section>
      <section className="login-panel">
        <form onSubmit={submit} className="login-form">
          <div className="login-mobile-brand">
            <Brand />
          </div>
          <p className="eyebrow">WELCOME HOME</p>
          <h2>欢迎回家</h2>
          <p className="muted">使用家庭共享账号，进入同一个媒体库。</p>
          <label htmlFor="username">家庭共享账号</label>
          <input
            id="username"
            name="username"
            autoComplete="username"
            required
            maxLength={80}
            autoFocus
            placeholder="输入账号"
          />
          <label htmlFor="password">密码</label>
          <input
            id="password"
            name="password"
            type="password"
            autoComplete="current-password"
            required
            maxLength={72}
            placeholder="输入密码"
          />
          {error && (
            <p role="alert" className="error">
              {error}
            </p>
          )}
          <button className="primary" disabled={busy}>
            {busy ? "正在登录…" : "进入媒体库 →"}
          </button>
          <p className="fine">
            家庭成员共用媒体库与观看进度。
            <br />
            账号由部署者配置，不提供注册。
          </p>
        </form>
      </section>
    </main>
  );
}

function App() {
  const [session, setSession] = useState<Session | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [navOpen, setNavOpen] = useState(false);
  const [libraryFilter, setLibraryFilter] =
    useState<LibraryFilter>(readLibraryFilter);
  useEffect(() => {
    if (!navOpen) return;
    document
      .querySelector<HTMLAnchorElement>("#main-navigation nav a")
      ?.focus();
    function closeOnEscape(event: KeyboardEvent) {
      if (event.key === "Escape") {
        setNavOpen(false);
        document.getElementById("navigation-toggle")?.focus();
      }
    }
    window.addEventListener("keydown", closeOnEscape);
    return () => window.removeEventListener("keydown", closeOnEscape);
  }, [navOpen]);
  useEffect(() => {
    api<Session>("/api/session")
      .then(setSession)
      .catch((error) => {
        if (error.status !== 401) setError(message(error));
      })
      .finally(() => setLoading(false));
  }, []);
  async function logout() {
    setError("");
    try {
      await api("/api/logout", {});
      window.history.replaceState(null, "", "/login");
      setLibraryFilter("all");
      setNavOpen(false);
      setSession(null);
    } catch (error) {
      setError(message(error));
    }
  }
  if (loading)
    return (
      <main className="loading" role="status">
        正在打开家庭放映室…
      </main>
    );
  if (!session)
    return (
      <>
        {error && (
          <p role="alert" className="error">
            {error}
          </p>
        )}
        <Login onLogin={setSession} />
      </>
    );
  const path = window.location.pathname;
  const directoryPage = path === "/directories";
  const settingsPage = path === "/settings/metadata";
  const managementPage = directoryPage || settingsPage;
  const seriesId = path.startsWith("/series/")
    ? path.slice("/series/".length)
    : null;
  const mediaId = path.startsWith("/media/")
    ? path.slice("/media/".length)
    : null;
  const personRoute = path.match(/^\/people\/(tmdb|tvdb|metatube)\/([^/]+)$/);
  const detailPage = Boolean(mediaId || seriesId || personRoute);
  const libraryPage = !detailPage && !managementPage;
  return (
    <div
      className={`app-shell${detailPage ? " detail-page" : ""}${libraryPage ? " home-page" : ""}${navOpen ? " navigation-open" : ""}`}
    >
      <a className="skip-link" href="#main-content">
        跳到主要内容
      </a>
      <aside className="sidebar" id="main-navigation">
        <Brand />
        <p className="nav-label">我的媒体</p>
        <nav aria-label="主导航">
          <a
            className={`nav-item ${!managementPage && libraryFilter === "all" ? "active" : ""}`}
            href="/library"
            title="媒体库"
            aria-current={
              !managementPage && libraryFilter === "all" ? "page" : undefined
            }
          >
            <Icon name="library" /> 媒体库
          </a>
          {(
            [
              ["movie", "电影", "film"],
              ["series", "电视剧", "tv"],
              ["unmatched", "未匹配媒体", "unmatched"],
              ["favorites", "收藏", "heart"],
              ["recent", "最近添加", "clock"],
            ] as const
          ).map(([value, label, icon]) => (
            <a
              key={value}
              title={label}
              href={`/library?type=${value}`}
              className={`nav-item ${!managementPage && !mediaId && !seriesId && libraryFilter === value ? "active" : ""}`}
              aria-current={
                !managementPage &&
                !mediaId &&
                !seriesId &&
                libraryFilter === value
                  ? "page"
                  : undefined
              }
            >
              <Icon name={icon} />
              {label}
            </a>
          ))}
          <p className="nav-label">管理</p>
          <a
            className={`nav-item ${settingsPage ? "active" : ""}`}
            href="/settings/metadata"
            title="影片资料源"
            aria-current={settingsPage ? "page" : undefined}
          >
            <Icon name="settings" /> 影片资料源
          </a>
          <a
            className={`nav-item ${directoryPage ? "active" : ""}`}
            href="/directories"
            title="媒体目录"
            aria-current={directoryPage ? "page" : undefined}
          >
            <Icon name="folder" /> 媒体目录
          </a>
        </nav>
        <div className="sidebar-foot">
          <Icon name="server" />
          <div>
            私享家庭放映室<p>媒体与进度，全家共享</p>
          </div>
        </div>
      </aside>
      {(detailPage || libraryPage) && navOpen && (
        <button
          className="navigation-scrim"
          aria-label="关闭导航"
          onClick={() => {
            setNavOpen(false);
            document.getElementById("navigation-toggle")?.focus();
          }}
        />
      )}
      <div className="app-main">
        <header className="app-header">
          <div className="header-start">
            {detailPage && (
              <>
                <a
                  className="icon-button"
                  href="/library"
                  aria-label="返回媒体库"
                  title="返回媒体库"
                >
                  <Icon name="back" />
                </a>
                <a
                  className="icon-button"
                  href="/library"
                  aria-label="首页"
                  title="首页"
                >
                  <Icon name="home" />
                </a>
              </>
            )}
            <button
              id="navigation-toggle"
              className="icon-button navigation-toggle"
              aria-label={navOpen ? "收起导航" : "打开导航"}
              aria-expanded={navOpen}
              aria-controls="main-navigation"
              onClick={() => setNavOpen(!navOpen)}
            >
              <Icon name={navOpen ? "close" : "menu"} />
            </button>
            {libraryPage ? (
              <a className="home-wordmark" href="/library" aria-label="首页">
                reel<span>nest</span>
              </a>
            ) : (
              <span className="header-title">
                {settingsPage
                  ? "影片资料源配置"
                  : directoryPage
                    ? "媒体目录管理"
                    : detailPage
                      ? "reelnest"
                      : "我的家庭影院"}
              </span>
            )}
          </div>
          <div className="account">
            <a
              className="icon-button"
              href="/library?focus=search"
              aria-label="搜索媒体"
              title="搜索媒体"
            >
              <Icon name="search" />
            </a>
            <span className="account-divider" aria-hidden="true" />
            <span className="avatar" aria-hidden="true">
              家
            </span>
            <span className="account-name">{session.username}</span>
            <button
              className="icon-button"
              onClick={logout}
              aria-label="退出登录"
              title="退出登录"
            >
              <Icon name="logout" />
            </button>
            {libraryPage && (
              <a
                className="icon-button"
                href="/directories"
                aria-label="管理媒体目录"
                title="管理媒体目录"
              >
                <Icon name="settings" />
              </a>
            )}
          </div>
          {libraryPage && (
            <nav className="home-tabs" aria-label="首页导航">
              {(
                [
                  ["all", "首页"],
                  ["favorites", "收藏"],
                  ["recent", "最近添加"],
                ] as const
              ).map(([value, label]) => (
                <a
                  key={value}
                  href={value === "all" ? "/library" : `/library?type=${value}`}
                  aria-current={libraryFilter === value ? "page" : undefined}
                >
                  {label}
                </a>
              ))}
            </nav>
          )}
        </header>
        <main className="content" id="main-content" tabIndex={-1}>
          {error && (
            <p role="alert" className="error">
              {error}
            </p>
          )}
          <>
            {settingsPage ? (
              <MetadataSettings />
            ) : directoryPage ? (
              <Directories />
            ) : personRoute ? (
              <PersonDetails
                source={personRoute[1]}
                personKey={personRoute[2]}
              />
            ) : seriesId ? (
              <SeriesDetails id={seriesId} />
            ) : mediaId ? (
              <MediaDetails id={mediaId} />
            ) : (
              <Library filter={libraryFilter} />
            )}
          </>
          {libraryPage ? (
            <details className="home-server-status">
              <summary>服务器状态</summary>
              <MetadataStatus />
            </details>
          ) : (
            <MetadataStatus />
          )}
        </main>
        <footer>
          <div>
            REEL NEST <span>让收藏回到生活</span>
          </div>
          <TMDBCredits />
        </footer>
      </div>
    </div>
  );
}

createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
);
