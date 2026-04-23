"use client";

import { EmptyState } from "@/components/shared/empty-state";
import { StatusBadge } from "@/components/shared/status-badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useRuns } from "@/lib/api/hooks";

// Stub /runs — pulls the list and renders the minimum viable table.
// Filters, pagination, and the row-click drill-down land in Phase 9.5.
export default function RunsPage() {
  const { data, isLoading, isError, error } = useRuns({ limit: 50 });
  const items = data?.items ?? [];

  return (
    <div className="space-y-4">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Runs</h1>
        <p className="text-sm text-muted-foreground">Verification jobs across every chain.</p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Recent runs</CardTitle>
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
              title="No runs yet"
              description="Schedule a verification run from the CLI or the forthcoming /runs/new form."
            />
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Status</TableHead>
                  <TableHead>Chain</TableHead>
                  <TableHead>Strategy</TableHead>
                  <TableHead>Trigger</TableHead>
                  <TableHead>Created</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.map((run) => (
                  <TableRow key={run.id}>
                    <TableCell>
                      <StatusBadge value={run.status} />
                    </TableCell>
                    <TableCell className="font-mono text-xs">{run.chain_id}</TableCell>
                    <TableCell className="font-mono text-xs">{run.strategy_kind}</TableCell>
                    <TableCell className="font-mono text-xs">{run.trigger_kind}</TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      {new Date(run.created_at).toLocaleString()}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
