import { useEffect, useRef, useSyncExternalStore } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import type { Schema } from "../../shared/api";
import { useAPI } from "../../shared/services";
import { invalidateMetadata } from "../catalog/cache";
import { keys } from "../catalog/queries";
import styles from "./tasks.module.css";

const subscribeVisibility = (callback: () => void) => {
  document.addEventListener("visibilitychange", callback);
  return () => document.removeEventListener("visibilitychange", callback);
};
export function taskInterval(data?: Schema["MetadataStatus"], failed = false) {
  return !failed && data && data.queued + data.running > 0 ? 1500 : 30000;
}
export function useTaskStatus() {
  const visible = useSyncExternalStore(
    subscribeVisibility,
    () => document.visibilityState !== "hidden",
    () => true,
  );
  const api = useAPI();
  const client = useQueryClient();
  const previous = useRef<number | undefined>(undefined);
  const query = useQuery({
    queryKey: keys.tasks,
    queryFn: ({ signal }) =>
      api("get", "/api/metadata/status", { query: { limit: 10 }, signal }),
    enabled: visible,
    retry: false,
    refetchInterval: (query) =>
      visible
        ? taskInterval(query.state.data, query.state.status === "error")
        : false,
    refetchIntervalInBackground: false,
    refetchOnReconnect: true,
  });
  useEffect(() => {
    if (!visible) void client.cancelQueries({ queryKey: keys.tasks });
  }, [visible, client]);
  useEffect(() => {
    const revision = query.data?.revision;
    if (revision === undefined) return;
    const prior = previous.current;
    previous.current = revision;
    if (prior !== undefined && revision !== prior)
      void invalidateMetadata(client);
  }, [query.data?.revision, client]);
  return query;
}
// Mounted exactly once by the authenticated shell, never by individual pages.
export function TaskStatus() {
  const query = useTaskStatus();
  const data = query.data;
  return (
    <div className={styles.status} role="status">
      {query.isError
        ? "资料任务状态暂不可用，将稍后重查"
        : !data
          ? "正在读取资料任务状态…"
          : data.queued + data.running > 0
            ? `影片资料：${data.running} 个处理中，${data.queued} 个等待中`
            : data.failed
              ? `影片资料：${data.failed} 个目标需稍后重试`
              : "影片资料任务空闲"}
    </div>
  );
}
