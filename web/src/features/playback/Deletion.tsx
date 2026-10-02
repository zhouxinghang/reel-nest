import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { APIError, type Schema } from "../../shared/api";
import { useAPI } from "../../shared/services";
import { Dialog } from "../../shared/Dialog";
import { Loading, QueryError, ui } from "../../shared/ui";
import { matchingAffected } from "../matching/cache";
import styles from "./playback.module.css";
export function deletionMessage(result: Schema["DeleteResult"]) {
  if (result.fileOutcome === "unknown")
    return "删除结果未知；不要重复删除，请检查文件或重新扫描核对。";
  if (result.fileOutcome === "not_deleted")
    return "源文件未删除。请核对原因，再重新预览。";
  return result.indexOutcome === "pending"
    ? "源文件已永久删除，但索引更新未完成。请重新扫描核对；不要再次删除。"
    : "源文件已永久删除，索引已更新。";
}
export function Deletion({
  id,
  libraryId,
  subjectId,
  onClose,
}: {
  id: string;
  libraryId: string;
  subjectId: string;
  onClose: (notice: string) => void;
}) {
  const api = useAPI();
  const client = useQueryClient();
  const [checked, setChecked] = useState(false);
  const [now, setNow] = useState(Date.now());
  const [notice, setNotice] = useState("");
  const preview = useQuery({
    queryKey: ["deletion-preview", id],
    gcTime: 0,
    staleTime: Infinity,
    retry: false,
    queryFn: ({ signal }) =>
      api("post", "/api/media/{id}/deletion-preview", {
        path: { id },
        body: {},
        signal,
      }),
  });
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, []);
  const deletion = useMutation({
    retry: false,
    mutationFn: () =>
      api("post", "/api/media/{id}/delete", {
        path: { id },
        body: { confirmation: preview.data!.confirmation, confirm: true },
      }),
    onSuccess: (result) =>
      setNotice(`${deletionMessage(result)} ${result.message}`),
    onError: (error) =>
      setNotice(
        error instanceof APIError && error.status >= 400 && error.status < 500
          ? `未执行删除：${error.message}。请关闭后重新核对预览。`
          : "删除结果未知（连接中断或响应未确认）；不要重复删除，请检查文件或重新扫描核对。",
      ),
    onSettled: async () => {
      const predicate = (q: Parameters<typeof matchingAffected>[0]) =>
        matchingAffected(q, subjectId, libraryId);
      await client.cancelQueries({ predicate });
      await client.invalidateQueries({ predicate });
    },
  });
  const close = () => {
    if (!deletion.isPending) onClose(notice);
  };
  const expired = !!preview.data && now >= preview.data.expiresAt;
  return (
    <Dialog title="删除源文件" onClose={close}>
      <QueryError error={preview.error} />
      {preview.isPending && <Loading />}
      {preview.data && (
        <>
          <p>将永久删除以下单个视频文件：</p>
          <p className={styles.path}>{preview.data.path}</p>
          <p className={styles.warning}>
            {preview.data.warning}{" "}
            本系统无法恢复；不会连带删除字幕、海报或文件夹。
          </p>
          <p>确认有效至 {new Date(preview.data.expiresAt).toLocaleString()}</p>
          {!notice && (
            <>
              <label>
                <input
                  type="checkbox"
                  checked={checked}
                  disabled={deletion.isPending}
                  onChange={(e) => setChecked(e.target.checked)}
                />
                我已核对路径，确认永久删除且本系统无法恢复
              </label>
              {expired && <p role="alert">确认已过期，请取消后重新预览。</p>}
              <div className={styles.actions}>
                <button
                  className={ui.button}
                  disabled={deletion.isPending}
                  onClick={close}
                >
                  取消
                </button>
                <button
                  className={ui.primary}
                  disabled={!checked || expired || deletion.isPending}
                  onClick={() => deletion.mutate()}
                >
                  永久删除此文件
                </button>
              </div>
            </>
          )}
        </>
      )}
      {deletion.isPending && (
        <p role="status">正在确认删除结果，请勿重复操作…</p>
      )}
      {notice && (
        <>
          <p role="status">{notice}</p>
          <button className={ui.button} onClick={close}>
            关闭结果
          </button>
        </>
      )}
    </Dialog>
  );
}
