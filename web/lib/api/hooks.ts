import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { api, type Schemas } from "@/lib/api/client";

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

export function useRun(id: string) {
  return useQuery({
    queryKey: ["run", id],
    queryFn: async ({ signal }) => {
      const { data, error } = await api.GET("/runs/{id}", {
        params: { path: { id } },
        signal,
      });
      if (error) throw new Error("get run failed");
      return data;
    },
    enabled: !!id,
  });
}

export function useRunDiffs(id: string) {
  return useQuery({
    queryKey: ["run-diffs", id],
    queryFn: async ({ signal }) => {
      const { data, error } = await api.GET("/runs/{id}/diffs", {
        params: { path: { id } },
        signal,
      });
      if (error) throw new Error("get run diffs failed");
      return data;
    },
    enabled: !!id,
  });
}

// useCancelRun posts to /runs/{id}/cancel and invalidates both the
// detail and list caches so the UI flips to "cancelled" without a
// hard refresh.
export function useCancelRun() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (id: string) => {
      const { error } = await api.POST("/runs/{id}/cancel", {
        params: { path: { id } },
      });
      if (error) throw new Error("cancel run failed");
    },
    onSuccess: (_, id) => {
      qc.invalidateQueries({ queryKey: ["run", id] });
      qc.invalidateQueries({ queryKey: ["runs"] });
    },
  });
}

// useCreateRun wraps POST /runs. The body is the exact
// CreateRunRequest schema the backend expects, fed from the /runs/new
// form. Success invalidates the runs list cache so the new row shows
// up when the user navigates back.
export function useCreateRun() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (body: Schemas["CreateRunRequest"]) => {
      const { data, error } = await api.POST("/runs", { body });
      if (error) throw new Error("create run failed");
      return data;
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["runs"] });
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
