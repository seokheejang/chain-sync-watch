"use client";

import { Pencil, Plus, Trash2 } from "lucide-react";
import { useMemo, useState } from "react";
import { toast } from "sonner";

import { EmptyState } from "@/components/shared/empty-state";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import type { Schemas } from "@/lib/api/client";
import {
  useChainSourceCounts,
  useChains,
  useCreateSource,
  useDeleteSource,
  useSources,
  useUpdateSource,
} from "@/lib/api/hooks";
import { cn } from "@/lib/utils";

import { SourceFormDialog, type SourceFormValues } from "./source-form-dialog";

type SourceRow = Schemas["SourceConfigView"];
type ChainEntry = Schemas["ChainView"];

// Fallback catalog used until /chains returns. Mirrors the 5 MVP
// entries in internal/config/defaults.yaml so the sidebar renders
// immediately on cold load.
const FALLBACK_CHAINS: ChainEntry[] = [
  { id: 1, slug: "ethereum", display_name: "Ethereum" },
  { id: 10, slug: "optimism", display_name: "Optimism" },
  { id: 8453, slug: "base", display_name: "Base" },
  { id: 42161, slug: "arbitrum", display_name: "Arbitrum" },
  { id: 11155111, slug: "sepolia", display_name: "Sepolia" },
];

export default function SourcesPage() {
  const { data: chainsData } = useChains();
  const chains: ChainEntry[] = useMemo(() => {
    const items = chainsData?.items ?? [];
    return items.length > 0 ? items : FALLBACK_CHAINS;
  }, [chainsData]);

  const [selectedChainId, setSelectedChainId] = useState<number>(() => chains[0]?.id ?? 10);
  // Keep selection valid whenever the catalog rehydrates — e.g. cold
  // load swapping fallback out for the API list.
  const selected = chains.find((c) => c.id === selectedChainId) ?? chains[0];
  const activeChainId = selected?.id ?? selectedChainId;

  // Per-chain source counts power the sidebar's configured/empty
  // dot. Shares ["sources", chainId] cache with useSources below, so
  // the active chain's table doesn't re-fetch.
  const { counts: chainSourceCounts } = useChainSourceCounts(chains.map((c) => c.id));

  const { data, isLoading, isError, error } = useSources(activeChainId);
  const items: SourceRow[] = data?.items ?? [];

  const [createOpen, setCreateOpen] = useState(false);
  const [editing, setEditing] = useState<SourceRow | null>(null);
  const [deleting, setDeleting] = useState<SourceRow | null>(null);

  const createMut = useCreateSource();
  const updateMut = useUpdateSource();
  const deleteMut = useDeleteSource();

  const onCreate = (values: SourceFormValues) => {
    createMut.mutate(
      {
        type: values.type,
        chain_id: activeChainId,
        endpoint: values.endpoint,
        api_key: values.api_key || undefined,
        options: values.archive ? { archive: true } : {},
      },
      {
        onSuccess: (res) => {
          toast.success(`Source ${res?.id ?? ""} created`);
          setCreateOpen(false);
        },
        onError: (err) => toast.error(err instanceof Error ? err.message : "Create source failed"),
      },
    );
  };

  const onUpdate = (values: SourceFormValues) => {
    if (!editing) return;
    updateMut.mutate(
      {
        id: editing.id,
        body: {
          endpoint: values.endpoint,
          api_key: values.api_key || undefined,
          clear_secret: values.clear_secret,
          options: values.archive ? { archive: true } : {},
          enabled: values.enabled,
        },
      },
      {
        onSuccess: () => {
          toast.success(`Source ${editing.id} updated`);
          setEditing(null);
        },
        onError: (err) => toast.error(err instanceof Error ? err.message : "Update source failed"),
      },
    );
  };

  const onDelete = () => {
    if (!deleting) return;
    deleteMut.mutate(deleting.id, {
      onSuccess: () => {
        toast.success(`Source ${deleting.id} deleted`);
        setDeleting(null);
      },
      onError: (err) => toast.error(err instanceof Error ? err.message : "Delete source failed"),
    });
  };

  return (
    <div className="space-y-4">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Sources</h1>
          <p className="text-sm text-muted-foreground">
            Pick a chain, then manage the RPC / Blockscout / Routescan / private-indexer rows for
            it. One row per (type, chain); to swap an endpoint, edit the existing row instead of
            creating a duplicate.
          </p>
        </div>
        <Button onClick={() => setCreateOpen(true)}>
          <Plus className="mr-2 h-4 w-4" /> New source
        </Button>
      </div>

      <div className="grid gap-4 md:grid-cols-[220px_1fr]">
        <Card>
          <CardHeader>
            <CardTitle>Chains</CardTitle>
          </CardHeader>
          <CardContent className="p-2">
            <nav className="flex flex-col gap-0.5" aria-label="Chain selector">
              {chains.map((c) => {
                const isActive = c.id === activeChainId;
                const sourceCount = chainSourceCounts[c.id] ?? 0;
                const configured = sourceCount > 0;
                return (
                  <button
                    type="button"
                    key={c.id}
                    onClick={() => setSelectedChainId(c.id)}
                    title={
                      configured
                        ? `${sourceCount} source${sourceCount === 1 ? "" : "s"} configured`
                        : "No sources configured"
                    }
                    className={cn(
                      "flex items-center justify-between rounded-md px-3 py-2 text-left text-sm transition-colors",
                      isActive ? "bg-muted font-medium" : "hover:bg-muted/50",
                    )}
                  >
                    <span className="flex items-center gap-2">
                      <span
                        aria-hidden
                        className={cn(
                          "inline-block h-2 w-2 rounded-full",
                          configured ? "bg-emerald-500" : "bg-muted-foreground/30",
                        )}
                      />
                      <span className={configured ? undefined : "text-muted-foreground"}>
                        {c.display_name}
                      </span>
                    </span>
                    <span className="font-mono text-xs text-muted-foreground">{c.id}</span>
                  </button>
                );
              })}
            </nav>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>{selected?.display_name ?? `Chain ${activeChainId}`} sources</CardTitle>
          </CardHeader>
          <CardContent>
            {isLoading ? (
              <div className="space-y-2">
                <Skeleton className="h-8 w-full" />
                <Skeleton className="h-8 w-full" />
                <Skeleton className="h-8 w-full" />
              </div>
            ) : isError ? (
              <EmptyState
                title="Couldn't reach the API"
                description={
                  error instanceof Error ? error.message : "Check NEXT_PUBLIC_API_BASE_URL."
                }
              />
            ) : items.length === 0 ? (
              <EmptyState
                title="No sources configured for this chain"
                description="Add RPC / Blockscout / Routescan / private-indexer rows manually, or run `make seed` for the Optimism defaults."
                action={
                  <Button onClick={() => setCreateOpen(true)}>
                    <Plus className="mr-2 h-4 w-4" /> New source
                  </Button>
                }
              />
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>ID</TableHead>
                    <TableHead>Type</TableHead>
                    <TableHead>Endpoint</TableHead>
                    <TableHead>Secret</TableHead>
                    <TableHead>Enabled</TableHead>
                    <TableHead className="w-24" />
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {items.map((s) => (
                    <TableRow key={s.id}>
                      <TableCell className="font-mono text-xs">{s.id}</TableCell>
                      <TableCell>
                        <Badge variant="outline">{s.type}</Badge>
                      </TableCell>
                      <TableCell className="font-mono text-xs">{s.endpoint}</TableCell>
                      <TableCell className="text-xs">
                        {s.has_secret ? (
                          <Badge variant="outline">set</Badge>
                        ) : (
                          <span className="text-muted-foreground">—</span>
                        )}
                      </TableCell>
                      <TableCell className="text-xs">
                        <span
                          className="inline-flex items-center gap-2"
                          title={s.enabled ? "enabled" : "disabled"}
                        >
                          <span
                            aria-hidden
                            className={cn(
                              "inline-block h-2 w-2 rounded-full",
                              s.enabled ? "bg-emerald-500" : "bg-red-500",
                            )}
                          />
                          <span className={s.enabled ? undefined : "text-muted-foreground"}>
                            {s.enabled ? "enabled" : "disabled"}
                          </span>
                        </span>
                      </TableCell>
                      <TableCell>
                        <div className="flex justify-end gap-1">
                          <Button
                            variant="ghost"
                            size="icon-sm"
                            aria-label={`Edit ${s.id}`}
                            onClick={() => setEditing(s)}
                          >
                            <Pencil className="h-3.5 w-3.5" />
                          </Button>
                          <Button
                            variant="ghost"
                            size="icon-sm"
                            aria-label={`Delete ${s.id}`}
                            onClick={() => setDeleting(s)}
                          >
                            <Trash2 className="h-3.5 w-3.5" />
                          </Button>
                        </div>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </CardContent>
        </Card>
      </div>

      <SourceFormDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        mode="create"
        defaultChainId={activeChainId}
        onSubmit={onCreate}
        pending={createMut.isPending}
      />
      <SourceFormDialog
        open={!!editing}
        onOpenChange={(open) => !open && setEditing(null)}
        mode="edit"
        source={editing}
        onSubmit={onUpdate}
        pending={updateMut.isPending}
      />

      <Dialog open={!!deleting} onOpenChange={(open) => !open && setDeleting(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete source</DialogTitle>
            <DialogDescription>
              Remove <code className="font-mono">{deleting?.id}</code>? Verification runs against
              this chain will lose this adapter. The row can be re-added by running{" "}
              <code className="font-mono">make seed</code> on a wiped table, or recreated from the
              New source form.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDeleting(null)}>
              Cancel
            </Button>
            <Button variant="destructive" onClick={onDelete} disabled={deleteMut.isPending}>
              {deleteMut.isPending ? "Deleting…" : "Delete"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
