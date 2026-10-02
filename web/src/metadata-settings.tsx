import { useEffect, useState, type FormEvent } from "react";
import { api, message } from "./api";
import { type MetadataSource } from "./metadata-sources";

type ProviderSettings = {
  id: MetadataSource;
  name: string;
  url: string;
  credentialSet: boolean;
  pinSet: boolean;
  configured: boolean;
  revision: number;
};
type SettingsResponse = { sources: ProviderSettings[] };
const credentialLabels = {
  tmdb: "TMDB API Read Access Token",
  tvdb: "TVDB API Key",
  metatube: "MetaTube Token",
};

export function MetadataSettings() {
  const [sources, setSources] = useState<ProviderSettings[]>([]);
  const [attempt, setAttempt] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [feedback, setFeedback] = useState("");
  const [saving, setSaving] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setError("");
    void api<SettingsResponse>("/api/settings/metadata", undefined, {
      signal: controller.signal,
    })
      .then((data) => {
        if (!controller.signal.aborted) setSources(data.sources);
      })
      .catch((error) => {
        if (!controller.signal.aborted) setError(message(error));
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [attempt]);
  return (
    <>
      <div className="page-heading">
        <div>
          <p className="eyebrow">TITLE METADATA</p>
          <h1>影片资料源</h1>
          <p className="muted">在这里配置服务，保存后即时生效，无需重启。</p>
        </div>
        <a className="secondary" href="/directories">
          管理媒体目录 →
        </a>
      </div>
      <p className="notice">
        配置由家庭共享账号管理。凭据加密保存在服务端，不回显；留空保持原值，清除需明确勾选。
        请通过 HTTPS 或可信局域网访问，并妥善保护完整应用数据备份。
      </p>
      <p className="fine">
        服务已配置不代表已验证连通性。各媒体库的启用和优先级仍在媒体目录中设置；保存不会重扫源文件，也不会覆盖手动匹配。
      </p>
      <button
        className="secondary"
        disabled={loading || saving > 0}
        onClick={() => {
          setLoading(true);
          setFeedback("");
          setAttempt((value) => value + 1);
        }}
      >
        {loading ? "正在加载配置…" : "重新加载配置（放弃未保存修改）"}
      </button>
      {error && (
        <p role="alert" className="error">
          {error}
        </p>
      )}
      {feedback && (
        <p role="status" className="notice">
          {feedback}
        </p>
      )}
      {!loading && !error && (
        <div className="metadata-settings-grid">
          {sources.map((source) => (
            <ProviderForm
              key={`${attempt}:${source.id}:${source.revision}`}
              source={source}
              saving={(busy) => setSaving((count) => count + (busy ? 1 : -1))}
              saved={(next) => {
                setSources((current) =>
                  current.map((item) => (item.id === next.id ? next : item)),
                );
                setFeedback(`${next.name} 配置已保存并生效。`);
                window.dispatchEvent(new Event("metadata-updated"));
              }}
            />
          ))}
        </div>
      )}
    </>
  );
}

function ProviderForm({
  source,
  saved,
  saving,
}: {
  source: ProviderSettings;
  saved: (next: ProviderSettings) => void;
  saving: (busy: boolean) => void;
}) {
  const [url, setURL] = useState(source.url);
  const [credential, setCredential] = useState("");
  const [pin, setPIN] = useState("");
  const [clearCredential, setClearCredential] = useState(false);
  const [clearPIN, setClearPIN] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const label = credentialLabels[source.id];
  const changed =
    url !== source.url ||
    credential.trim() !== "" ||
    pin.trim() !== "" ||
    clearCredential ||
    clearPIN;
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy(true);
    saving(true);
    setError("");
    try {
      const next = await api<ProviderSettings>(
        `/api/settings/metadata/${source.id}`,
        {
          revision: source.revision,
          ...(source.id === "metatube" ? { url } : {}),
          credential,
          clearCredential,
          ...(source.id === "tvdb" ? { pin, clearPin: clearPIN } : {}),
        },
      );
      setCredential("");
      setPIN("");
      saved(next);
    } catch (error) {
      setError(message(error));
    } finally {
      saving(false);
      setBusy(false);
    }
  }
  return (
    <form
      className="panel metadata-provider-form"
      aria-label={`${source.name} 配置`}
      onSubmit={submit}
    >
      <h2>{source.name}</h2>
      <p className="fine">
        {source.configured ? "服务已配置" : "尚未配置服务"}
      </p>
      <fieldset disabled={busy}>
        {source.id === "metatube" && (
          <>
            <label htmlFor="metatube-url">MetaTube 服务地址</label>
            <input
              id="metatube-url"
              type="url"
              value={url}
              onChange={(event) => setURL(event.target.value)}
              maxLength={2048}
              placeholder="http://192.168.1.20:8080"
              autoComplete="off"
            />
            <p className="fine">
              仅电影；连接已有服务，不负责部署。留空地址禁用服务。更换地址时必须重新填写或明确清除已有
              Token。
            </p>
          </>
        )}
        <label htmlFor={`${source.id}-credential`}>{label}</label>
        <input
          id={`${source.id}-credential`}
          type="password"
          value={credential}
          onChange={(event) => setCredential(event.target.value)}
          autoComplete="new-password"
          spellCheck={false}
          maxLength={4096}
          disabled={clearCredential}
          placeholder={
            source.credentialSet
              ? "已保存，留空保持不变"
              : source.id === "metatube"
                ? "仅无鉴权服务可省略"
                : "输入凭据"
          }
        />
        <p className="fine">
          {source.credentialSet ? "凭据已保存，不回显。" : "尚未保存凭据。"}
        </p>
        {source.credentialSet && (
          <label className="settings-clear">
            <input
              type="checkbox"
              checked={clearCredential}
              onChange={(event) => {
                setClearCredential(event.target.checked);
                if (event.target.checked) setCredential("");
              }}
            />
            清除已保存的 {label}
          </label>
        )}
        {source.id === "tvdb" && (
          <>
            <label htmlFor="tvdb-pin">TVDB PIN</label>
            <input
              id="tvdb-pin"
              type="password"
              value={pin}
              onChange={(event) => setPIN(event.target.value)}
              autoComplete="new-password"
              spellCheck={false}
              maxLength={4096}
              disabled={clearPIN}
              placeholder={
                source.pinSet ? "已保存，留空保持不变" : "仅订阅型 API Key 需要"
              }
            />
            <p className="fine">
              {source.pinSet
                ? "PIN 已保存，不回显。"
                : "未保存 PIN；非订阅型 API Key 可省略。"}
            </p>
            {source.pinSet && (
              <label className="settings-clear">
                <input
                  type="checkbox"
                  checked={clearPIN}
                  onChange={(event) => {
                    setClearPIN(event.target.checked);
                    if (event.target.checked) setPIN("");
                  }}
                />
                清除已保存的 TVDB PIN
              </label>
            )}
          </>
        )}
        {source.id !== "metatube" && (
          <p className="fine">
            清除 {source.id === "tmdb" ? "Token" : "API Key"}{" "}
            后停止从此来源匹配资料，已缓存资料保留。
          </p>
        )}
        <button className="primary" disabled={busy || !changed}>
          {busy ? "正在保存…" : "保存配置"}
        </button>
      </fieldset>
      {error && (
        <p className="error" role="alert">
          {error}
        </p>
      )}
    </form>
  );
}
