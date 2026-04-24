import { useMutation, useQueries, useQuery, useQueryClient } from "@tanstack/react-query";

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

export function useDiff(id: string) {
  return useQuery({
    queryKey: ["diff", id],
    queryFn: async ({ signal }) => {
      const { data, error } = await api.GET("/diffs/{id}", {
        params: { path: { id } },
        signal,
      });
      if (error) throw new Error("get diff failed");
      return data;
    },
    enabled: !!id,
  });
}

// useReplayDiff re-runs the comparison for a persisted discrepancy
// using the same (metric, block, source set) it originally saw. A
// "resolved" result means the sources now agree (the backend
// marks the record resolved); a "new_diff_id" points at a fresh
// row when they still disagree. Invalidates the diff list + the
// specific record so the caller sees the new state without a
// manual refresh.
export function useReplayDiff() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (id: string) => {
      const { data, error } = await api.POST("/diffs/{id}/replay", {
        params: { path: { id } },
      });
      if (error) throw new Error("replay diff failed");
      return data;
    },
    onSuccess: (_, id) => {
      qc.invalidateQueries({ queryKey: ["diff", id] });
      qc.invalidateQueries({ queryKey: ["diffs"] });
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

export function useCreateSchedule() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (body: Schemas["CreateScheduleRequest"]) => {
      const { data, error } = await api.POST("/schedules", { body });
      if (error) throw new Error("create schedule failed");
      return data;
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["schedules"] });
    },
  });
}

// useCancelSchedule deactivates a recurring job. DELETE returns 204,
// so the mutation resolves with void; callers invalidate the list
// cache via onSuccess to flip the "active" column without a refetch
// round-trip.
export function useCancelSchedule() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (id: string) => {
      const { error } = await api.DELETE("/schedules/{id}", {
        params: { path: { id } },
      });
      if (error) throw new Error("cancel schedule failed");
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["schedules"] });
    },
  });
}

// useChainSourceCounts fans /sources?chain_id=X out across every
// chain id passed in. The queryKey matches useSources so the two
// hooks share the TanStack cache — the sidebar summary and the
// detail table never double-fetch the same page. Callers get a
// plain {chainId: count} map so the UI layer doesn't have to know
// the query shape.
export function useChainSourceCounts(chainIds: number[]) {
  const results = useQueries({
    queries: chainIds.map((id) => ({
      queryKey: ["sources", id],
      queryFn: async ({ signal }: { signal: AbortSignal }) => {
        const { data, error } = await api.GET("/sources", {
          params: { query: { chain_id: id } },
          signal,
        });
        if (error) throw new Error("list sources failed");
        return data;
      },
      enabled: id > 0,
    })),
  });
  const counts: Record<number, number> = {};
  chainIds.forEach((id, idx) => {
    counts[id] = results[idx].data?.items?.length ?? 0;
  });
  const isLoading = results.some((r) => r.isLoading);
  return { counts, isLoading };
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

export function useSource(id: string) {
  return useQuery({
    queryKey: ["source", id],
    queryFn: async ({ signal }) => {
      const { data, error } = await api.GET("/sources/{id}", {
        params: { path: { id } },
        signal,
      });
      if (error) throw new Error("get source failed");
      return data;
    },
    enabled: !!id,
  });
}

// useSourceCapabilities materialises the source and reports its
// capability matrix. Split from useSource because it depends on
// runtime adapter instantiation; listing rows for the CRUD table
// doesn't need it.
// useSourceTypes returns the adapter type strings the backend's
// gateway.Registry currently knows about. Private-build
// deployments add entries here transparently — the UI's type
// dropdown always reflects the running binary.
export function useSourceTypes() {
  return useQuery({
    queryKey: ["source-types"],
    queryFn: async ({ signal }) => {
      const { data, error } = await api.GET("/sources/types", { signal });
      if (error) throw new Error("list source types failed");
      return data;
    },
    staleTime: 60_000,
  });
}

export function useSourceCapabilities(id: string) {
  return useQuery({
    queryKey: ["source-capabilities", id],
    queryFn: async ({ signal }) => {
      const { data, error } = await api.GET("/sources/{id}/capabilities", {
        params: { path: { id } },
        signal,
      });
      if (error) throw new Error("get source capabilities failed");
      return data;
    },
    enabled: !!id,
  });
}

export function useCreateSource() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (body: Schemas["CreateSourceRequest"]) => {
      const { data, error } = await api.POST("/sources", { body });
      if (error) throw new Error("create source failed");
      return data;
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["sources"] });
    },
  });
}

export function useUpdateSource() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async ({ id, body }: { id: string; body: Schemas["UpdateSourceRequest"] }) => {
      const { data, error } = await api.PUT("/sources/{id}", {
        params: { path: { id } },
        body,
      });
      if (error) throw new Error("update source failed");
      return data;
    },
    onSuccess: (_, { id }) => {
      qc.invalidateQueries({ queryKey: ["sources"] });
      qc.invalidateQueries({ queryKey: ["source", id] });
    },
  });
}

export function useDeleteSource() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (id: string) => {
      const { error } = await api.DELETE("/sources/{id}", {
        params: { path: { id } },
      });
      if (error) throw new Error("delete source failed");
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["sources"] });
    },
  });
}

// useChains returns the chain catalog the backend advertises. Served
// from embedded defaults.yaml through /chains so the frontend's
// dropdowns and the sources sidebar stay in sync with the binary's
// known chain list without a duplicated hardcoded map.
export function useChains() {
  return useQuery({
    queryKey: ["chains"],
    queryFn: async ({ signal }) => {
      const { data, error } = await api.GET("/chains", { signal });
      if (error) throw new Error("list chains failed");
      return data;
    },
    staleTime: 5 * 60_000,
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
