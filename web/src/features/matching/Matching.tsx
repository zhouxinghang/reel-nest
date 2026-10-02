import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { APIError, type Schema } from "../../shared/api";
import { useAPI } from "../../shared/services";
import { Dialog } from "../../shared/Dialog";
import { Loading, QueryError, ui } from "../../shared/ui";
import { confirmMatch, matchingAffected } from "./cache";
import styles from "./matching.module.css";

export function Matching({
  match,
  libraryId,
  title,
  onClose,
}: {
  match: Schema["Match"];
  libraryId: string;
  title: string;
  onClose: () => void;
}) {
  const api = useAPI();
  const client = useQueryClient();
  const [source, setSource] = useState<Schema["Source"]>(
    match.selection?.remote.source || "tmdb",
  );
  const [draft, setDraft] = useState(title);
  const [search, setSearch] = useState<{
    q: string;
    source: Schema["Source"];
  } | null>(null);
  const [stale, setStale] = useState(false);
  const query = useQuery({
    queryKey: ["candidates", match.subjectId, search],
    gcTime: 0,
    enabled: !!search,
    retry: false,
    queryFn: ({ signal }) =>
      api("get", "/api/metadata/subjects/{id}/candidates", {
        path: { id: match.subjectId },
        query: search!,
        signal,
      }),
  });
  const selection = useMutation({
    mutationFn: (candidate: Schema["Candidate"]) =>
      api("put", "/api/metadata/subjects/{id}/selection", {
        path: { id: match.subjectId },
        body: {
          subjectRevision: query.data!.subjectRevision,
          expectedRevision: query.data!.bindingRevision,
          ref: candidate.ref,
        },
      }),
    onMutate: () =>
      client.cancelQueries({
        predicate: (q) => matchingAffected(q, match.subjectId, libraryId),
      }),
    onSuccess: async (confirmed) => {
      await confirmMatch(client, libraryId, confirmed);
      onClose();
    },
    onError: async (error) => {
      if (
        error instanceof APIError &&
        ["CANDIDATE_STALE", "REVISION_CONFLICT"].includes(error.code)
      )
        setStale(true);
      await client.invalidateQueries({
        predicate: (q) => matchingAffected(q, match.subjectId, libraryId),
      });
    },
  });
  return (
    <Dialog
      title="匹配影片资料"
      onClose={() => {
        if (!selection.isPending) onClose();
      }}
    >
      <p className={ui.muted}>
        当前选择：
        {match.selection
          ? `${match.selection.remote.source} / ${match.selection.remote.remoteId}`
          : "无"}
        。只有服务器确认后才替换；取消或失败保留原关联。
      </p>
      <form
        onSubmit={async (e) => {
          e.preventDefault();
          selection.reset();
          setStale(false);
          if (search?.q === draft.trim() && search.source === source)
            await query.refetch();
          else setSearch({ q: draft.trim(), source });
        }}
      >
        <label>
          资料来源
          <select
            className={ui.input}
            value={source}
            disabled={selection.isPending}
            onChange={(e) => setSource(e.target.value as Schema["Source"])}
          >
            <option value="tmdb">TMDB</option>
            <option value="tvdb">TVDB</option>
            <option value="metatube">MetaTube</option>
          </select>
        </label>
        <label>
          片名或编号
          <input
            className={ui.input}
            value={draft}
            maxLength={200}
            required
            disabled={selection.isPending}
            onChange={(e) => setDraft(e.target.value)}
          />
        </label>
        <button
          className={ui.primary}
          disabled={selection.isPending || query.isFetching}
        >
          搜索候选
        </button>
      </form>
      <QueryError error={query.error || selection.error} />
      {query.isFetching && <Loading />}
      {stale && (
        <p role="alert">
          候选或选择版本已变化，请重新搜索；不会自动覆盖当前选择。
        </p>
      )}
      {query.data && !query.isFetching && (
        <>
          {!query.data.complete && (
            <p>来源只返回了部分候选，不代表唯一匹配。</p>
          )}
          {!query.data.items.length && <p>没有找到候选。</p>}
          <div className={styles.candidates}>
            {query.data.items.map((candidate, i) => (
              <section key={i}>
                <h3>
                  {candidate.title} {candidate.year || ""}
                </h3>
                <p>{candidate.overview}</p>
                <p className={ui.muted}>
                  {candidate.ref.namespace} / {candidate.ref.remoteId}
                </p>
                <button
                  className={ui.button}
                  disabled={stale || query.isError || selection.isPending}
                  onClick={() => selection.mutate(candidate)}
                >
                  选择 {candidate.title}
                </button>
              </section>
            ))}
          </div>
        </>
      )}
      {selection.isPending && (
        <p role="status">正在验证资料并提交选择，请稍候…</p>
      )}
    </Dialog>
  );
}
