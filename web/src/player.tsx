import { useEffect, useRef, useState } from "react";
import { api, message } from "./api";
import type { MediaFile, WatchProgress } from "./media";
import { Subtitles } from "./subtitles";
import { Icon } from "./icons";
import "./player.css";

export function clock(seconds: number) {
  return `${Math.floor(seconds / 60)}:${String(Math.floor(seconds % 60)).padStart(2, "0")}`;
}

export function Player({
  file,
  fromStart = false,
  onClose,
}: {
  file: MediaFile;
  fromStart?: boolean;
  onClose: () => void;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
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
  const [autoplayBlocked, setAutoplayBlocked] = useState(false);
  const [closing, setClosing] = useState(false);
  const closingRef = useRef(false);
  const base = `/api/media/${encodeURIComponent(file.id)}`;

  useEffect(() => {
    const element = dialog.current!;
    element.showModal();
    document.documentElement.classList.add("web-player-open");
    return () => {
      element.close();
      document.documentElement.classList.remove("web-player-open");
    };
  }, []);

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
    if (!progress) return queue.current;
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
    return queue.current;
  }
  async function close() {
    if (closingRef.current) return;
    closingRef.current = true;
    setClosing(true);
    video.current?.pause();
    await save();
    dialog.current?.close();
    onClose();
  }
  useEffect(() => {
    let active = true;
    api<WatchProgress>(`${base}/playback`)
      .then((progress) => {
        if (active) {
          initial.current = fromStart ? null : progress;
          setReady(true);
        }
      })
      .catch((error) => {
        if (active) setError(message(error));
      });
    return () => {
      active = false;
    };
  }, [base, fromStart]);
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
  async function start() {
    const element = video.current;
    if (!element) return;
    try {
      await element.play();
    } catch (error) {
      if (!element.isConnected || closingRef.current) return;
      if (element.error) await diagnose();
      else if (
        error instanceof DOMException &&
        error.name === "NotAllowedError"
      )
        setAutoplayBlocked(true);
      else if (!(error instanceof DOMException && error.name === "AbortError"))
        setError(`无法开始播放：${message(error)}；请重试或使用外部播放器。`);
    }
  }
  function toggle() {
    if (video.current?.paused) void start();
    else video.current?.pause();
  }
  return (
    <dialog
      ref={dialog}
      className="player web-player"
      aria-label="网页播放器"
      onCancel={(event) => {
        event.preventDefault();
        void close();
      }}
    >
      <header className="web-player-header">
        <div>
          <h2>{file.match.metadata?.title || file.filename}</h2>
          <span>网页全屏 · 原始视频直放 · 观看进度全家共用</span>
        </div>
        <button
          className="icon-button"
          aria-label="关闭播放器"
          title="返回详情（Esc）"
          disabled={closing}
          onClick={() => void close()}
        >
          <Icon name="close" />
        </button>
      </header>
      <div className="web-player-stage">
        {error && (
          <p role="alert" className="error web-player-notice">
            {error}
          </p>
        )}
        {autoplayBlocked && (
          <p role="status" className="notice web-player-notice">
            浏览器阻止了自动播放，请点击下方“开始播放”。
          </p>
        )}
        {!ready && !error && <p role="status">正在检查文件与观看进度…</p>}
        {ready && (
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
              setPosition(element.currentTime);
              // Start only after the saved position has been restored.
              void start();
            }}
            onPlaying={() => {
              started.current = true;
              setPlaying(true);
              setError("");
              setAutoplayBlocked(false);
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
        )}
      </div>
      <div className="web-player-console">
        {ready && (
          <>
            <div className="web-player-toolbar">
              <div className="player-controls">
                <button className="primary" disabled={closing} onClick={toggle}>
                  {playing ? "暂停" : "开始播放"}
                </button>
                <span>
                  {clock(position)} /{" "}
                  {clock(Number.isFinite(duration) ? duration : 0)}
                </span>
              </div>
              <Subtitles id={file.id} video={video} />
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
        {closing && (
          <p role="status" className="fine">
            正在保存进度并返回详情…
          </p>
        )}
      </div>
    </dialog>
  );
}
