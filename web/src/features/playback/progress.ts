import type { Schema } from "../../shared/api";
type Snapshot = Schema["ProgressWrite"];
// A single writer and a single replaceable pending slot, not a Promise chain.
// Requests are never replayed on failure. Cross-device arbitration stays server-side.
export class ProgressWriter {
  private pending: Snapshot | null = null;
  private running: Promise<void> | null = null;
  private controller = new AbortController();
  private stopped = false;
  private confirmed = true;
  constructor(
    private write: (value: Snapshot, signal: AbortSignal) => Promise<unknown>,
    private report: (saved: boolean) => void = () => {},
  ) {}
  submit(value: Snapshot) {
    if (this.stopped) return;
    this.pending = value;
    this.confirmed = false;
    if (!this.running) this.running = this.drain();
  }
  private async drain() {
    await Promise.resolve();
    while (this.pending && !this.stopped) {
      const value = this.pending;
      this.pending = null;
      let ok = false;
      try {
        await this.write(value, this.controller.signal);
        ok = true;
      } catch {
        /* No automatic retry. */
      }
      if (!this.stopped) {
        this.confirmed = ok && !this.pending;
        this.report(ok);
      }
    }
    this.running = null;
  }
  async finish(timeoutMs = 3000) {
    let timer: ReturnType<typeof setTimeout> | undefined;
    const done = await Promise.race([
      (this.running || Promise.resolve()).then(() => this.confirmed),
      new Promise<false>((resolve) => {
        timer = setTimeout(() => resolve(false), timeoutMs);
      }),
    ]);
    clearTimeout(timer);
    this.stop();
    return done;
  }
  stop() {
    this.stopped = true;
    this.pending = null;
    this.controller.abort();
  }
}
export function progressSnapshot(
  video: Pick<HTMLVideoElement, "currentTime" | "duration">,
  ended: boolean,
): Snapshot | null {
  if (
    !Number.isFinite(video.duration) ||
    video.duration <= 0 ||
    !Number.isFinite(video.currentTime)
  )
    return null;
  return {
    position: Math.max(0, Math.min(video.currentTime, video.duration)),
    duration: video.duration,
    completed: ended && video.currentTime >= video.duration - 0.5,
  };
}
export type PlayerState =
  | "checking"
  | "ready"
  | "playing"
  | "paused"
  | "blocked"
  | "failed"
  | "closing";
export function playerTransition(
  current: PlayerState,
  next: PlayerState,
): PlayerState {
  return current === "closing" ? current : next;
}
export function mediaError(code?: number) {
  return code === 2
    ? "视频读取中断，请检查网络或文件状态。"
    : code === 3 || code === 4
      ? "浏览器无法解码此视频，请尝试外部播放器（本系统不转码）。"
      : "视频加载失败，请检查源文件或尝试外部播放器。";
}
