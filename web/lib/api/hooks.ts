import { useQuery } from "@tanstack/react-query";

import { api } from "@/lib/api/client";

// Hooks are thin wrappers around openapi-fetch + TanStack Query so
// pages consume the API with one-liners and share a single cache.
// Each hook keys on the resource + its query params so independent
// pages with overlapping filters don't refetch the same data.

export function useRuns(params?: {
  chain_id?: number;
  status?: string;
  limit?: number;
  offset?: number;
}) {
  return useQuery({
    queryKey: ["runs", params],
    queryFn: async ({ signal }) => {
      const { data, error } = await api.GET("/runs", { params: { query: params }, signal });
      if (error) throw new Error("list runs failed");
      return data;
    },
  });
}

export function useDiffs(params?: {
  run_id?: string;
  metric_key?: string;
  severity?: string;
  resolved?: string;
  limit?: number;
  offset?: number;
}) {
  return useQuery({
    queryKey: ["diffs", params],
    queryFn: async ({ signal }) => {
      const { data, error } = await api.GET("/diffs", { params: { query: params }, signal });
      if (error) throw new Error("list diffs failed");
      return data;
    },
  });
}

export function useSchedules() {
  return useQuery({
    queryKey: ["schedules"],
    queryFn: async ({ signal }) => {
      const { data, error } = await api.GET("/schedules", { signal });
      if (error) throw new Error("list schedules failed");
      return data;
    },
  });
}

export function useSources(chainId: number) {
  return useQuery({
    queryKey: ["sources", chainId],
    queryFn: async ({ signal }) => {
      const { data, error } = await api.GET("/sources", {
        params: { query: { chain_id: chainId } },
        signal,
      });
      if (error) throw new Error("list sources failed");
      return data;
    },
    enabled: chainId > 0,
  });
}

// Server readiness — dashboard uses this for a "backend reachable"
// indicator. Intentionally short staleTime so operators get fast
// feedback when the API goes down.
export function useReadiness() {
  return useQuery({
    queryKey: ["readyz"],
    queryFn: async ({ signal }) => {
      const { data, error } = await api.GET("/readyz", { signal });
      if (error) throw new Error("readyz failed");
      return data;
    },
    staleTime: 5_000,
    refetchInterval: 15_000,
  });
}
