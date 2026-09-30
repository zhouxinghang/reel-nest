import React, { useEffect, useState, type FormEvent } from "react";
import { createRoot } from "react-dom/client";
import { api, message } from "./api";
import "./style.css";
import { Library, Directories, MediaDetails } from "./library";
import { SeriesDetails } from "./series";

type Session = { username: string };

function Brand() {
  return (
    <a className="brand" href="/library">
      <span className="brand-mark" aria-hidden="true">
        ▥
      </span>
      <span>
        reel nest<span className="brand-sub">家庭媒体库</span>
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
  const seriesId = path.startsWith("/series/")
    ? path.slice("/series/".length)
    : null;
  const mediaId = path.startsWith("/media/")
    ? path.slice("/media/".length)
    : null;
  return (
    <div className="app-shell">
      <aside className="sidebar">
        <Brand />
        <p className="nav-label">浏览</p>
        <nav>
          <a
            className={`nav-item ${!directoryPage ? "active" : ""}`}
            href="/library"
          >
            <span aria-hidden="true">▦</span> 媒体库
          </a>
          <a
            className={`nav-item ${directoryPage ? "active" : ""}`}
            href="/directories"
          >
            <span aria-hidden="true">▱</span> 媒体目录
          </a>
        </nav>
        <div className="sidebar-foot">
          <span className="status-dot" /> 私享家庭放映室
          <p>原始媒体留在你的设备上</p>
        </div>
      </aside>
      <div className="app-main">
        <header>
          <span className="header-title">属于全家的好时光</span>
          <div className="account">
            <span className="avatar" aria-hidden="true">
              家
            </span>
            <span>{session.username}</span>
            <button className="text-button" onClick={logout}>
              退出登录
            </button>
          </div>
        </header>
        <main className="content">
          {error && (
            <p role="alert" className="error">
              {error}
            </p>
          )}
          <>
            {directoryPage ? (
              <Directories />
            ) : seriesId ? (
              <SeriesDetails id={seriesId} />
            ) : mediaId ? (
              <MediaDetails id={mediaId} />
            ) : (
              <Library />
            )}
          </>
        </main>
        <footer>
          REEL NEST <span>让收藏回到生活</span>
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
