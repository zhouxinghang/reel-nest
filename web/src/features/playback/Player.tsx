import { Dialog } from "../../shared/Dialog";
import { ui } from "../../shared/ui";
import { usePlayer } from "./usePlayer";
import { Subtitles } from "./Subtitles";
import styles from "./playback.module.css";
export function Player({
  id,
  title,
  libraryId,
  fromStart,
  onClose,
}: {
  id: string;
  title: string;
  libraryId: string;
  fromStart: boolean;
  onClose: (notice: string) => void;
}) {
  const p = usePlayer(id, libraryId, fromStart, onClose);
  return (
    <Dialog title={`播放 ${title}`} wide onClose={() => void p.close()}>
      <div data-player-state={p.state}>
        {p.state === "checking" && (
          <p role="status">正在核对文件与家庭共用进度…</p>
        )}
        {p.playback && (
          <>
            <video
              className={styles.video}
              ref={p.video}
              src={p.playback.contentPath}
              controls
              playsInline
              preload="metadata"
              {...p.events}
            />
            {p.state === "blocked" && (
              <p role="status">浏览器阻止了自动播放，请点击播放。</p>
            )}
            {p.state !== "playing" && p.state !== "closing" && (
              <button className={ui.primary} onClick={() => void p.play()}>
                播放视频
              </button>
            )}
            <Subtitles id={id} video={p.video} />
          </>
        )}
        {p.error && (
          <p role="alert" className={ui.error}>
            {p.error}
          </p>
        )}
        {p.saved && <p role="status">{p.saved}</p>}
        {p.state === "closing" && (
          <p role="status">正在保存进度，最多等待 3 秒…</p>
        )}
        <button
          className={ui.button}
          disabled={p.state === "closing"}
          onClick={() => void p.close()}
        >
          关闭播放器
        </button>
        <p className={ui.muted}>
          家庭共用进度；离开页面仅尽力保存，原生播放结束才标记已看完。
        </p>
      </div>
    </Dialog>
  );
}
