import { useEffect, useRef, useState } from "react";
import { api, APIError, message } from "./api";
import { Icon } from "./icons";

type Preview = { path: string; confirmation: string; expiresAt: number };
type Outcome = { deleted: boolean; indexUpdated: boolean; message: string };

export function DeleteSourceFile({
  id,
  onDeleted,
  compact = false,
}: {
  id: string;
  onDeleted: () => void;
  compact?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const [outcome, setOutcome] = useState<Outcome | null>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    if (open) return () => trigger.current?.focus();
  }, [open]);
  return (
    <section className="deletion-entry" aria-label="单文件永久删除">
      {!outcome ? (
        <button
          ref={trigger}
          className={
            compact ? "detail-icon-action detail-delete" : "danger secondary"
          }
          aria-label="删除源文件"
          title="删除源文件"
          onClick={() => setOpen(true)}
        >
          {compact ? <Icon name="trash" /> : "删除源文件"}
        </button>
      ) : (
        <p
          role={outcome.indexUpdated ? "status" : "alert"}
          className={outcome.indexUpdated ? "notice" : "error"}
        >
          {outcome.message}
        </p>
      )}
      {open && (
        <DeletionDialog
          id={id}
          close={() => setOpen(false)}
          deleted={(result) => {
            setOutcome(result);
            setOpen(false);
            onDeleted();
          }}
        />
      )}
    </section>
  );
}

function DeletionDialog({
  id,
  close,
  deleted,
}: {
  id: string;
  close: () => void;
  deleted: (result: Outcome) => void;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  const submitting = useRef(false);
  const [preview, setPreview] = useState<Preview | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [attempted, setAttempted] = useState(false);
  const base = `/api/media/${encodeURIComponent(id)}`;
  useEffect(() => {
    dialog.current?.showModal();
    const controller = new AbortController();
    void api<Preview>(
      `${base}/deletion-preview`,
      {},
      { signal: controller.signal },
    )
      .then((value) => {
        if (!controller.signal.aborted) setPreview(value);
      })
      .catch((error) => {
        if (!controller.signal.aborted) setError(message(error));
      });
    return () => controller.abort();
  }, [base]);
  async function confirm() {
    if (!preview || submitting.current || attempted) return;
    submitting.current = true;
    setBusy(true);
    setAttempted(true);
    try {
      const result = await api<Outcome>(`${base}/delete`, {
        confirmation: preview.confirmation,
        confirmed: true,
      });
      if (!result.deleted) throw new Error("服务端未确认文件结果");
      deleted(result);
    } catch (error) {
      setError(
        error instanceof APIError
          ? message(error)
          : `删除结果未确认：${message(error)}。请重新扫描核对实际文件状态，不要直接重复删除。`,
      );
    } finally {
      submitting.current = false;
      setBusy(false);
    }
  }
  return (
    <dialog
      ref={dialog}
      className="matching-dialog deletion-dialog"
      aria-labelledby="deletion-title"
      aria-describedby="deletion-warning"
      onCancel={(event) => {
        event.preventDefault();
        if (!submitting.current) close();
      }}
    >
      <h2 id="deletion-title">永久删除这一个视频？</h2>
      <p id="deletion-warning" className="error">
        将永久删除磁盘或 NAS 上的源文件，本系统无法恢复。
      </p>
      <p>仅删除下列视频，不会删除字幕、海报、其他视频或目录。</p>
      {preview ? (
        <>
          <h3>实际目标路径（应用可见）</h3>
          <p className="deletion-path">{preview.path}</p>
          <p className="fine">
            请核对完整路径。确认有效期为 10 分钟，文件变化时将拒绝删除。
          </p>
        </>
      ) : !error ? (
        <p role="status">正在核对实际文件，此时不会删除…</p>
      ) : null}
      {error && (
        <p role="alert" className="error">
          {error}
        </p>
      )}
      {busy && (
        <p role="status">
          正在删除，请等待实际结果。此时不能取消已提交的删除。
        </p>
      )}
      <div className="matching-actions">
        <button className="secondary" autoFocus disabled={busy} onClick={close}>
          {attempted ? "关闭" : "取消"}
        </button>
        <button
          className="danger primary"
          disabled={!preview || busy || attempted}
          onClick={confirm}
        >
          {busy ? "正在删除…" : "确认永久删除"}
        </button>
      </div>
    </dialog>
  );
}
