import { useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { APIError, type Schema } from "../../shared/api";
import { useAPI } from "../../shared/services";
import { Loading, QueryError, ui } from "../../shared/ui";
import { keys, queries } from "../catalog/queries";
import { invalidateMetadata } from "../catalog/cache";
import styles from "./settings.module.css";

export function ProviderForm({
  initial,
}: {
  initial: Schema["ProviderSettings"];
}) {
  const api = useAPI();
  const client = useQueryClient();
  // Background query updates never replace this form's baseline or draft.
  const [baseline, setBaseline] = useState(initial);
  const [url, setURL] = useState(initial.url || "");
  const [credential, setCredential] = useState("");
  const [pin, setPin] = useState("");
  const [clearCredential, setClearCredential] = useState(false);
  const [clearPin, setClearPin] = useState(false);
  const [notice, setNotice] = useState("");
  const save = useMutation({
    mutationFn: (body: Schema["SettingsPatch"]) =>
      api("patch", "/api/settings/metadata/{source}", {
        path: { source: initial.source },
        body,
      }),
    onSuccess: (saved) => {
      setBaseline(saved);
      setCredential("");
      setPin("");
      setClearCredential(false);
      setClearPin(false);
      setURL(saved.url || "");
      setNotice("设置已保存；配置齐备不代表已验证连通性。");
      void client.invalidateQueries({ queryKey: keys.settings });
      void client.invalidateQueries({ queryKey: keys.tasks });
      void invalidateMetadata(client);
    },
  });
  const reload = useMutation({
    mutationFn: () => api("get", "/api/settings/metadata", {}),
    onSuccess: (settings) => {
      const latest = settings.sources.find((s) => s.source === initial.source);
      if (!latest) return;
      setBaseline(latest);
      save.reset();
      setNotice(
        `已读取版本 ${latest.revision}（当前地址：${latest.url || "固定来源地址"}）。草稿未变，请核对后再次保存。`,
      );
    },
  });
  const conflict =
    save.error instanceof APIError && save.error.code === "REVISION_CONFLICT";
  const busy = save.isPending || reload.isPending;
  function submit(event: FormEvent) {
    event.preventDefault();
    if (busy || conflict) return;
    setNotice("");
    const body: Schema["SettingsPatch"] = {
      revision: baseline.revision,
      credential,
      clearCredential,
    };
    if (initial.source === "tvdb") {
      body.pin = pin;
      body.clearPin = clearPin;
    }
    if (initial.source === "metatube") body.url = url;
    save.mutate(body);
  }
  return (
    <form
      className={styles.form}
      onSubmit={submit}
      aria-label={`${initial.name} 设置`}
    >
      <h2>{initial.name}</h2>
      <p className={ui.muted}>
        版本 {baseline.revision} ·{" "}
        {baseline.configured ? "配置齐备（未验证连通性）" : "尚未配置齐备"} ·{" "}
        {baseline.credentialSet ? "已有凭据" : "未保存凭据"}
      </p>
      <fieldset disabled={busy}>
        {initial.source === "metatube" && (
          <label>
            服务地址
            <input
              className={ui.input}
              value={url}
              onChange={(e) => setURL(e.target.value)}
              maxLength={2048}
              placeholder="http://nas:8080/v1"
            />
          </label>
        )}
        <label>
          {initial.source === "tvdb" ? "API Key" : "Token"}
          <input
            className={ui.input}
            type="password"
            autoComplete="new-password"
            value={credential}
            onChange={(e) => setCredential(e.target.value)}
            maxLength={4096}
            placeholder="留空保留已保存凭据"
          />
        </label>
        <label className={styles.check}>
          <input
            type="checkbox"
            checked={clearCredential}
            onChange={(e) => setClearCredential(e.target.checked)}
          />
          明确清除已保存凭据
        </label>
        {initial.source === "tvdb" && (
          <>
            <label>
              PIN
              <input
                className={ui.input}
                type="password"
                autoComplete="new-password"
                value={pin}
                onChange={(e) => setPin(e.target.value)}
                maxLength={4096}
                placeholder="留空保留已保存 PIN"
              />
            </label>
            <label className={styles.check}>
              <input
                type="checkbox"
                checked={clearPin}
                onChange={(e) => setClearPin(e.target.checked)}
              />
              明确清除已保存 PIN
            </label>
          </>
        )}
        {initial.source === "metatube" && (
          <p className={ui.muted}>
            更换地址时须重新输入 Token 或明确清除；不会自动携带旧 Token。
          </p>
        )}
        <button className={ui.primary} disabled={conflict}>
          保存 {initial.name}
        </button>
      </fieldset>
      <QueryError error={save.error} />
      <QueryError error={reload.error} />
      {conflict && (
        <>
          <p>其他会话修改了配置，草稿已保留。请先读取最新版本再确认保存。</p>
          <button
            className={ui.button}
            type="button"
            disabled={busy}
            onClick={() => reload.mutate()}
          >
            读取最新版本，保留草稿
          </button>
        </>
      )}
      {notice && <p role="status">{notice}</p>}
    </form>
  );
}
export function Settings() {
  const query = useQuery(queries.settings(useAPI()));
  return (
    <>
      <h1>影片资料源</h1>
      <p className={ui.muted}>
        家庭共享设置；凭据不会回显，媒体库分别决定来源顺序。
      </p>
      <QueryError error={query.error} retry={() => void query.refetch()} />
      {query.isPending && <Loading />}
      {query.data?.sources.map((source) => (
        <ProviderForm key={source.source} initial={source} />
      ))}
    </>
  );
}
