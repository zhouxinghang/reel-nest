import { useState } from "react";
import { api, message } from "./api";

export function ExternalPlayer({ id }: { id: string }) {
  const [url, setURL] = useState("");
  const [error, setError] = useState("");
  const [feedback, setFeedback] = useState("");
  const [busy, setBusy] = useState(false);
  async function create() {
    setBusy(true);
    setError("");
    try {
      const result = await api<{ path: string }>(
        `/api/media/${encodeURIComponent(id)}/stream-link`,
        {},
      );
      setURL(new URL(result.path, window.location.origin).href);
    } catch (error) {
      setError(message(error));
    } finally {
      setBusy(false);
    }
  }
  async function copy() {
    try {
      await navigator.clipboard.writeText(url);
      setFeedback("已复制串流地址");
    } catch {
      setFeedback("剪贴板不可用，请选中下方完整地址并手动复制。");
    }
  }
  const local = ["localhost", "127.0.0.1", "[::1]"].includes(
    window.location.hostname,
  );
  return (
    <section className="external-player" aria-label="外部播放器">
      <h2>使用外部播放器</h2>
      <p className="fine">
        浏览器无法直放？可交给观看设备上的 IINA（1.4.4+）或
        PotPlayer。不会更新网页观看进度，外挂字幕需自行加载。
      </p>
      <button
        className="secondary"
        disabled={busy}
        onClick={() => void create()}
      >
        {busy ? "正在获取…" : "获取串流地址"}
      </button>
      {error && (
        <p role="alert" className="error">
          {error}
        </p>
      )}
      {url && (
        <>
          <p className="notice">
            持有链接者可读取此视频，链接不会随时间自动失效；仅用于可信局域网，不要公开分享。
          </p>
          {local && (
            <p className="notice">
              当前地址仅供本机使用。若在其他设备播放，请使用服务器局域网 IP
              或域名访问网页后重新获取链接。
            </p>
          )}
          <div className="media-actions">
            <a
              className="secondary"
              href={`iina://open?url=${encodeURIComponent(url)}`}
            >
              用 IINA 打开
            </a>
            <a className="secondary" href={`potplayer://${url}`}>
              用 PotPlayer 打开
            </a>
            <button className="secondary" onClick={() => void copy()}>
              复制串流地址
            </button>
          </div>
          <label htmlFor={`stream-${id}`}>串流地址（可手动复制）</label>
          <input
            id={`stream-${id}`}
            className="stream-url"
            readOnly
            value={url}
            onFocus={(event) => event.currentTarget.select()}
          />
          <p className="fine">
            浏览器可能询问是否打开应用。若没有反应，请复制地址，在播放器中“打开
            URL”；PotPlayer
            并非所有安装方式都注册网页协议，网页不能确认唤起是否成功。
          </p>
          {feedback && (
            <p role="status" className="fine">
              {feedback}
            </p>
          )}
        </>
      )}
    </section>
  );
}
