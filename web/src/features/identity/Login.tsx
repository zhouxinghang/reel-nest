import { useState, type FormEvent } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate, useSearchParams } from "react-router-dom";
import { useAPI } from "../../shared/services";
import type { Schema } from "../../shared/api";
import { QueryError, ui } from "../../shared/ui";
import { keys } from "../catalog/queries";
import styles from "./identity.module.css";

export function safeReturnPath(value: string | null) {
  return value?.startsWith("/") &&
    !value.startsWith("//") &&
    !value.includes("\\") &&
    !value.startsWith("/login")
    ? value
    : "/";
}
export function Login() {
  const api = useAPI();
  const client = useQueryClient();
  const navigate = useNavigate();
  const [search] = useSearchParams();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const login = useMutation({
    mutationFn: (body: Schema["Login"]) => api("post", "/api/login", { body }),
    onSuccess: async (session) => {
      api.cancelAll();
      await client.cancelQueries();
      client.clear();
      client.setQueryData(keys.session, session);
      await navigate(safeReturnPath(search.get("next")), { replace: true });
    },
  });
  function submit(event: FormEvent) {
    event.preventDefault();
    login.mutate({ username, password });
  }
  return (
    <main className={styles.shell}>
      <section className={styles.story}>
        <p className={styles.brand}>
          reel<span>nest</span>
        </p>
        <h1>
          好故事，
          <br />
          在家一起看。
        </h1>
        <p>你的媒体 · 你的设备 · 一起观看</p>
      </section>
      <form className={styles.form} onSubmit={submit}>
        <p className={ui.muted}>WELCOME HOME</p>
        <h2>欢迎回家</h2>
        <p className={ui.muted}>使用家庭共享账号，进入同一个媒体库。</p>
        <label>
          家庭共享账号
          <input
            className={ui.input}
            autoFocus
            autoComplete="username"
            required
            maxLength={80}
            value={username}
            onChange={(e) => setUsername(e.target.value)}
          />
        </label>
        <label>
          密码
          <input
            className={ui.input}
            type="password"
            autoComplete="current-password"
            required
            maxLength={72}
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
        </label>
        <QueryError error={login.error} />
        <button className={ui.primary} disabled={login.isPending}>
          {login.isPending ? "正在登录…" : "进入媒体库 →"}
        </button>
      </form>
    </main>
  );
}
