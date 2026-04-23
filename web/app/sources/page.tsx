"use client";

import { Pencil, Plus, Trash2 } from "lucide-react";
import { useState } from "react";
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
import { useCreateSource, useDeleteSource, useSources, useUpdateSource } from "@/lib/api/hooks";

import { SourceFormDialog, type SourceFormValues } from "./source-form-dialog";

// Optimism is the MVP target chain. When multi-chain lands this
// becomes a chain picker at the top of the page.
const DEFAULT_CHAIN_ID = 10;

type SourceRow = Schemas["SourceConfigView"];

export default function SourcesPage() {
  const { data, isLoading, isError, error } = useSources(DEFAULT_CHAIN_ID);
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
        chain_id: values.chain_id,
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
            Adapter configuration for chain {DEFAULT_CHAIN_ID}. RPC + Blockscout + Routescan seed
            automatically from defaults.yaml; edit, enable/disable, or rotate credentials here.
          </p>
        </div>
        <Button onClick={() => setCreateOpen(true)}>
          <Plus className="mr-2 h-4 w-4" /> New source
        </Button>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Configured sources</CardTitle>
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
              title="No sources configured"
              description="Run `make seed` once to import defaults, or add a source manually."
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
                      {s.enabled ? (
                        <Badge variant="outline">enabled</Badge>
                      ) : (
                        <span className="text-muted-foreground">disabled</span>
                      )}
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

      <SourceFormDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        mode="create"
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
