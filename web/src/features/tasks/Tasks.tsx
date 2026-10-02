import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useSearchParams } from "react-router-dom";
import { useAPI } from "../../shared/services";
import {
  Loading,
  QueryError,
  Pagination,
  ui,
  useStaleCursor,
} from "../../shared/ui";
export function Tasks() {
  const api = useAPI();
  const client = useQueryClient();
  const [search] = useSearchParams();
  const cursor = search.get("cursor") || "";
  // Failure pages are on-demand reads, never a second status poller.
  const query = useQuery({
    queryKey: ["task-failures", { cursor }],
    gcTime: 0,
    queryFn: ({ signal }) =>
      api("get", "/api/metadata/status", {
        query: { cursor, limit: 50 },
        signal,
      }),
  });
  useStaleCursor(query.error);
  const refresh = useMutation({
    mutationFn: () => api("post", "/api/metadata/refresh", { body: {} }),
    onSuccess: async () => {
      await client.invalidateQueries({ queryKey: ["tasks"] });
      await query.refetch();
    },
  });
  return (
    <>
      <h1>影片资料任务</h1>
      <p className={ui.muted}>
        刷新资料与扫描源文件相互独立；手动选择不会因失败被清除。
      </p>
      <button
        className={ui.primary}
        disabled={refresh.isPending}
        onClick={() => refresh.mutate()}
      >
        刷新全部影片资料
      </button>
      <button className={ui.button} onClick={() => void query.refetch()}>
        更新失败列表
      </button>
      <QueryError error={refresh.error || query.error} />
      {query.isPending && <Loading />}
      {query.data && (
        <>
          <p>
            等待 {query.data.queued} · 处理中 {query.data.running} · 失败{" "}
            {query.data.failed}
          </p>
          <h2>失败目标</h2>
          {query.data.failures.map((job) => (
            <p key={job.subjectId}>
              {job.subjectId} · {job.errorCode} · 已尝试 {job.attempts} 次
            </p>
          ))}
          {!query.data.failures.length && <p>当前没有失败目标。</p>}
          <Pagination page={query.data.page} />
        </>
      )}
    </>
  );
}
