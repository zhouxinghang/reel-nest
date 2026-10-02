import { useEffect, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { errorMessage, isCancelled, type Schema } from "../../shared/api";
import { useAPI } from "../../shared/services";
import {
  ProgressWriter,
  progressSnapshot,
  playerTransition,
  mediaError,
  type PlayerState,
} from "./progress";
import { favoriteAffected } from "../catalog/cache";

export function usePlayer(
  id: string,
  libraryId: string,
  fromStart: boolean,
  onClose: (notice: string) => void,
) {
  const api = useAPI();
  const client = useQueryClient();
  const video = useRef<HTMLVideoElement>(null);
  const [state, setState] = useState<PlayerState>("checking");
  const stateRef = useRef<PlayerState>("checking");
  const change = (next: PlayerState) => {
    stateRef.current = playerTransition(stateRef.current, next);
    setState(stateRef.current);
  };
  const [playback, setPlayback] = useState<Schema["Playback"] | null>(null);
  const initial = useRef<Schema["Progress"] | null>(null);
  const [error, setError] = useState("");
  const [saved, setSaved] = useState("");
  const ready = useRef(false);
  const started = useRef(false);
  const restored = useRef(false);
  const ended = useRef(false);
  const alive = useRef(true);
  const writer = useRef<ProgressWriter | null>(null);
  const save = () => {
    if (!ready.current || !started.current || !video.current) return;
    const value = progressSnapshot(video.current, ended.current);
    if (value) writer.current?.submit(value);
  };
  useEffect(() => {
    alive.current = true;
    const controller = new AbortController();
    writer.current = new ProgressWriter(
      (body, signal) =>
        api("put", "/api/media/{id}/progress", { path: { id }, body, signal }),
      (ok) => {
        if (alive.current)
          setSaved(
            ok
              ? "家庭共用进度已保存"
              : "进度保存未确认；网络恢复后的新进度可再次提交",
          );
      },
    );
    void api("get", "/api/media/{id}/playback", {
      path: { id },
      signal: controller.signal,
    })
      .then((data) => {
        if (!alive.current) return;
        initial.current =
          fromStart || data.progress?.completed ? null : data.progress;
        setPlayback(data);
        change("ready");
      })
      .catch((e) => {
        if (alive.current && !isCancelled(e)) {
          setError(errorMessage(e));
          change("failed");
        }
      });
    const timer = setInterval(() => {
      if (
        video.current &&
        !video.current.paused &&
        stateRef.current !== "closing"
      )
        save();
    }, 10000);
    const hidden = () => {
      if (
        document.visibilityState === "hidden" &&
        stateRef.current !== "closing"
      )
        save();
    };
    const leaving = () => save();
    document.addEventListener("visibilitychange", hidden);
    window.addEventListener("pagehide", leaving);
    return () => {
      // Navigation/unmount is best effort only, never an unbounded close barrier.
      save();
      alive.current = false;
      controller.abort();
      clearInterval(timer);
      document.removeEventListener("visibilitychange", hidden);
      window.removeEventListener("pagehide", leaving);
      const pending = writer.current;
      void pending?.finish();
    };
  }, [id, api, fromStart]);
  async function play() {
    if (!video.current || !ready.current || stateRef.current === "closing")
      return;
    try {
      await video.current.play();
    } catch (e) {
      if (!alive.current || (stateRef.current as PlayerState) === "closing")
        return;
      if (
        e &&
        typeof e === "object" &&
        "name" in e &&
        e.name === "NotAllowedError"
      )
        change("blocked");
      else if (!isCancelled(e)) {
        change("failed");
        setError(mediaError(video.current.error?.code));
      }
    }
  }
  function restore() {
    if (!video.current || restored.current) return;
    restored.current = true;
    const position = initial.current?.position || 0;
    if (position > 0 && Number.isFinite(video.current.duration))
      video.current.currentTime = Math.min(
        position,
        Math.max(0, video.current.duration - 0.5),
      );
    else {
      ready.current = true;
      void play();
    }
  }
  async function close() {
    if (stateRef.current === "closing") return;
    change("closing");
    video.current?.pause();
    const valid =
      video.current && progressSnapshot(video.current, ended.current);
    save();
    const confirmed = await writer.current?.finish();
    if (!alive.current) return;
    void client.invalidateQueries({
      predicate: (q) =>
        favoriteAffected(q, { id, entryType: "media", libraryId }),
    });
    onClose(
      !started.current
        ? "已关闭播放器；未开始播放，没有写入进度。"
        : confirmed && valid
          ? "已关闭播放器，进度保存完成。"
          : "已关闭播放器；本次进度保存未确认，下次播放请核对位置。",
    );
  }
  return {
    video,
    state,
    playback,
    error,
    saved,
    play,
    close,
    events: {
      onLoadedMetadata: restore,
      onSeeked: () => {
        if (!ready.current && restored.current) {
          ready.current = true;
          void play();
        } else {
          ended.current = false;
          save();
        }
      },
      onPlay: () => {
        if (!ready.current || stateRef.current === "closing") {
          video.current?.pause();
          return;
        }
        started.current = true;
        ended.current = false;
        change("playing");
      },
      onPause: () => {
        if (ready.current && stateRef.current !== "closing") {
          change("paused");
          save();
        }
      },
      onEnded: () => {
        ended.current = true;
        change("paused");
        save();
      },
      onError: () => {
        change("failed");
        setError(mediaError(video.current?.error?.code));
      },
    },
  };
}
