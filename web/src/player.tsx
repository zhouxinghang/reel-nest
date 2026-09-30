import { useEffect, useRef, useState } from "react";
import { api, message } from "./api";
import type { MediaFile, WatchProgress } from "./media";
import { Subtitles } from "./subtitles";

export function clock(seconds: number) {
  return `${Math.floor(seconds / 60)}:${String(Math.floor(seconds % 60)).padStart(2, "0")}`;
}

export function Player({ file }: { file: MediaFile }) {
  const video = useRef<HTMLVideoElement>(null);
  const initial = useRef<WatchProgress | null>(null);
  const started = useRef(false);
  const lastSave = useRef(0);
  const queue = useRef(Promise.resolve());
  const [ready, setReady] = useState(false);
  const [error, setError] = useState("");
  const [saveError, setSaveError] = useState("");
  const [saved, setSaved] = useState("");
  const [position, setPosition] = useState(0);
  const [duration, setDuration] = useState(0);
  const [playing, setPlaying] = useState(false);
  const base = `/api/media/${encodeURIComponent(file.id)}`;

  function snapshot() {
    const element = video.current;
    if (
      !element ||
      !started.current ||
      !Number.isFinite(element.duration) ||
      element.duration <= 0
    )
      return null;
    return {
      position: Math.min(element.currentTime, element.duration),
      duration: element.duration,
      completed: element.ended,
    };
  }
  function save() {
    const progress = snapshot();
    if (!progress) return;
    lastSave.current = Date.now();
    // Serialize this player's writes so a slow previous request cannot overwrite
    // a more recent seek/ended event. Other browsers use last-received semantics.
    queue.current = queue.current.then(async () => {
      try {
        await api(`${base}/progress`, progress, { keepalive: true });
        setSaveError("");
        setSaved(
          progress.completed
            ? "已看完；再次播放从头开始"
            : `家庭共用进度已保存：${clock(progress.position)}`,
        );
      } catch (error) {
        setSaveError(`进度未保存：${message(error)}`);
      }
    });
  }
  useEffect(() => {
    let active = true;
    api<WatchProgress>(`${base}/playback`)
      .then((progress) => {
        if (active) {
          initial.current = progress;
          setReady(true);
        }
      })
      .catch((error) => {
        if (active) setError(message(error));
      });
    return () => {
      active = false;
    };
  }, [base]);
  useEffect(() => {
    // A best-effort final snapshot; abrupt process termination can still lose
    // the last interval. Never rely on unload as the only persistence point.
    const flush = () => save();
    const hidden = () => {
      if (document.visibilityState === "hidden") flush();
    };
    window.addEventListener("pagehide", flush);
    document.addEventListener("visibilitychange", hidden);
    return () => {
      window.removeEventListener("pagehide", flush);
      document.removeEventListener("visibilitychange", hidden);
    };
  }, [base]);
  async function diagnose() {
    const code = video.current?.error?.code;
    try {
      await api(`${base}/playback`);
      setError(
        code === 2
          ? "视频网络读取失败，请检查连接后重新打开播放器。"
          : "浏览器无法解码此视频，容器或编码可能不兼容。请使用 IINA 或 PotPlayer 外部播放器；本系统不转码。",
      );
    } catch (error) {
      setError(message(error));
    }
  }
  async function toggle() {
    const element = video.current;
    if (!element) return;
    if (element.paused) {
      try {
        await element.play();
      } catch (error) {
        if (element.error) await diagnose();
        else
          setError(`无法开始播放：${message(error)}；请重试或使用外部播放器。`);
      }
    } else element.pause();
  }
  return (
    <section className="player" aria-label="网页播放器">
      <h2>家庭放映室</h2>
      <p className="muted small">原始视频直放 · 不转码 · 观看进度全家共用</p>
      {error && (
        <p role="alert" className="error">
          {error}
        </p>
      )}
      {ready && (
        <>
          <video
            ref={video}
            controls
            playsInline
            preload="metadata"
            src={`${base}/content`}
            onLoadedMetadata={() => {
              const element = video.current!;
              setDuration(element.duration);
              const progress = initial.current;
              if (
                progress &&
                !progress.completed &&
                progress.position < element.duration
              )
                element.currentTime = progress.position;
            }}
            onPlaying={() => {
              started.current = true;
              setPlaying(true);
              setError("");
            }}
            onPause={() => {
              setPlaying(false);
              save();
            }}
            onEnded={() => {
              setPlaying(false);
              save();
            }}
            onSeeked={save}
            onTimeUpdate={() => {
              setPosition(video.current?.currentTime || 0);
              if (Date.now() - lastSave.current >= 10000) save();
            }}
            onError={() => void diagnose()}
          />
          <Subtitles id={file.id} video={video} />
          <div className="player-controls">
            <button className="primary" onClick={() => void toggle()}>
              {playing ? "暂停" : "开始播放"}
            </button>
            <span>
              {clock(position)} /{" "}
              {clock(Number.isFinite(duration) ? duration : 0)}
            </span>
          </div>
          <label htmlFor="playback-position">播放位置</label>
          <input
            id="playback-position"
            type="range"
            min={0}
            max={Number.isFinite(duration) ? duration : 0}
            step={0.1}
            value={position}
            onChange={(event) => {
              const value = Number(event.target.value);
              setPosition(value);
              if (video.current) video.current.currentTime = value;
            }}
          />
          {initial.current &&
            initial.current.position > 0 &&
            !initial.current.completed && (
              <p className="fine">
                从家庭共用进度 {clock(initial.current.position)} 继续。
              </p>
            )}
        </>
      )}
      {!ready && !error && <p role="status">正在检查文件与观看进度…</p>}
      {saved && (
        <p role="status" className="fine">
          {saved}
        </p>
      )}
      {saveError && (
        <p role="alert" className="error">
          {saveError}
        </p>
      )}
      <p className="fine">
        每 10
        秒及暂停、定位完成、播放结束时保存。异常关闭可能丢失最后一小段进度；多处观看以最后收到的有效更新为准。
      </p>
    </section>
  );
}
