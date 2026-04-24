"use client";

import { Plus } from "lucide-react";
import Link from "next/link";
import { useState } from "react";
import { toast } from "sonner";

import { EmptyState } from "@/components/shared/empty-state";
import { Badge } from "@/components/ui/badge";
import { Button, buttonVariants } from "@/components/ui/button";
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
import { useCancelSchedule, useSchedules } from "@/lib/api/hooks";
import { chainName } from "@/lib/chains";

export default function SchedulesPage() {
  const { data, isLoading, isError, error } = useSchedules();
  const cancel = useCancelSchedule();
  const [pendingCancel, setPendingCancel] = useState<{ jobId: string; cron: string } | null>(null);
  const items = data?.items ?? [];

  const confirmCancel = () => {
    if (!pendingCancel) return;
    const { jobId } = pendingCancel;
    cancel.mutate(jobId, {
      onSuccess: () => {
        toast.success(`Schedule deactivated — ${jobId}`);
        setPendingCancel(null);
      },
      onError: (err) => {
        toast.error(err instanceof Error ? err.message : "Cancel schedule failed");
      },
    });
  };

  return (
    <div className="space-y-4">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Schedules</h1>
          <p className="text-sm text-muted-foreground">
            Recurring verification jobs driven by cron expressions.
          </p>
        </div>
        <Link href="/schedules/new" className={buttonVariants()}>
          <Plus className="mr-2 h-4 w-4" /> New schedule
        </Link>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Active schedules</CardTitle>
        </CardHeader>
        <CardContent>
          {isLoading ? (
            <div className="space-y-2">
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
              title="No active schedules"
              description="Register a recurring job with the New schedule button above."
              action={
                <Link href="/schedules/new" className={buttonVariants()}>
                  <Plus className="mr-2 h-4 w-4" /> New schedule
                </Link>
              }
            />
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Job</TableHead>
                  <TableHead>Chain</TableHead>
                  <TableHead>Cron</TableHead>
                  <TableHead>Timezone</TableHead>
                  <TableHead>Metrics</TableHead>
                  <TableHead>Active</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.map((s) => (
                  <TableRow key={s.job_id}>
                    <TableCell className="font-mono text-xs" title={s.job_id}>
                      {s.job_id}
                    </TableCell>
                    <TableCell className="text-xs" title={`chain id ${s.chain_id}`}>
                      {chainName(s.chain_id)}
                    </TableCell>
                    <TableCell className="font-mono text-xs">{s.cron_expr}</TableCell>
                    <TableCell className="font-mono text-xs">{s.timezone || "UTC"}</TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      {(s.metric_keys ?? []).length === 0
                        ? "—"
                        : (s.metric_keys ?? []).slice(0, 3).join(", ") +
                          ((s.metric_keys ?? []).length > 3
                            ? ` +${(s.metric_keys ?? []).length - 3}`
                            : "")}
                    </TableCell>
                    <TableCell>
                      <Badge variant={s.active ? "default" : "secondary"}>
                        {s.active ? "Active" : "Inactive"}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-right">
                      <Button
                        variant="outline"
                        size="sm"
                        disabled={!s.active}
                        onClick={() => setPendingCancel({ jobId: s.job_id, cron: s.cron_expr })}
                      >
                        Deactivate
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      <Dialog
        open={pendingCancel !== null}
        onOpenChange={(open) => {
          if (!open) setPendingCancel(null);
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Deactivate schedule?</DialogTitle>
            <DialogDescription>
              Stops future ticks of <span className="font-mono text-xs">{pendingCancel?.cron}</span>
              . Past runs and discrepancies stay in the database — only the dispatcher entry is
              removed. This cannot be re-activated; recreate the schedule to resume.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setPendingCancel(null)}>
              Keep active
            </Button>
            <Button variant="destructive" onClick={confirmCancel} disabled={cancel.isPending}>
              {cancel.isPending ? "Deactivating…" : "Deactivate"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
