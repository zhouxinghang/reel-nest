import { createContext, useContext } from "react";
import { QueryClient } from "@tanstack/react-query";
import { APIError, type API } from "./api";

const APIContext = createContext<API | null>(null);
export const APIProvider = APIContext.Provider;
export function useAPI() {
  const api = useContext(APIContext);
  if (!api) throw new Error("APIProvider missing");
  return api;
}
export function createQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: {
        staleTime: 15000,
        gcTime: 60000,
        retry: (count, error) =>
          count < 1 &&
          error instanceof APIError &&
          error.retryable &&
          error.status !== 401,
        refetchOnWindowFocus: false,
      },
      // In particular, a failed delete/selection is never replayed automatically.
      mutations: { retry: false, networkMode: "always" },
    },
  });
}
