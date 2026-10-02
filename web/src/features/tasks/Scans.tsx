import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { useQueries, useQueryClient } from "@tanstack/react-query";
import type { Schema } from "../../shared/api";
import { useAPI } from "../../shared/services";
import { invalidateMetadata } from "../catalog/cache";
import { usePageVisible } from "./TaskStatus";

type Scan = Schema["Scan"];
const active = (s?: Scan) =>
  !!s && (s.state === "queued" || s.state === "running");
const Context = createContext<{
  watch: (ids: string[]) => void;
  scans: Map<string, Scan>;
  errors: Set<string>;
}>({ watch: () => {}, scans: new Map(), errors: new Set() });
export const useScans = () => useContext(Context);
// One shell-owned subscription, bounded to the currently visited 50-library page.
// Navigating away does not lose that page's ongoing scan feedback.
export function ScanProvider({ children }: { children: ReactNode }) {
  const [ids, setIDs] = useState<string[]>([]);
  const watch = useCallback(
    (next: string[]) =>
      setIDs((old) => {
        const bounded = [...new Set(next)].slice(0, 50);
        return old.join() === bounded.join() ? old : bounded;
      }),
    [],
  );
  const api = useAPI();
  const client = useQueryClient();
  const visible = usePageVisible();
  const queries = useQueries({
    queries: ids.map((id) => ({
      queryKey: ["scan", id],
      gcTime: 0,
      enabled: visible,
      retry: false,
      queryFn: ({ signal }: { signal: AbortSignal }) =>
        api("get", "/api/scans/{id}", { path: { id }, signal }),
      refetchInterval: (q: { state: { data?: Scan; status: string } }) =>
        !visible
          ? (false as const)
          : q.state.status === "error"
            ? 30000
            : active(q.state.data)
              ? 1500
              : (false as const),
    })),
  });
  const prior = useRef(new Map<string, string>());
  const scans = new Map(
    queries.flatMap((q) => (q.data ? [[q.data.id, q.data] as const] : [])),
  );
  const stamp = [...scans.values()].map((s) => `${s.id}:${s.state}`).join();
  useEffect(() => {
    if (!visible) void client.cancelQueries({ queryKey: ["scan"] });
  }, [visible, client]);
  useEffect(() => {
    let changed = false;
    for (const scan of scans.values()) {
      const previous = prior.current.get(scan.id);
      if (previous && previous !== scan.state && !active(scan)) changed = true;
    }
    prior.current = new Map([...scans.values()].map((s) => [s.id, s.state]));
    if (changed) {
      void invalidateMetadata(client);
      void client.invalidateQueries({ queryKey: ["tasks"] });
    }
    // The bounded snapshot's identity changes each render; its state stamp is stable.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [stamp, client]);
  return (
    <Context.Provider
      value={{
        watch,
        scans,
        errors: new Set(ids.filter((_, i) => queries[i].isError)),
      }}
    >
      {children}
    </Context.Provider>
  );
}
export function ScanSummary() {
  const { scans } = useScans();
  const count = [...scans.values()].filter(active).length;
  return count ? (
    <span role="status">扫描：{count} 个已查看媒体库处理中</span>
  ) : null;
}
