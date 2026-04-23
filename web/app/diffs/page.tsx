"use client";

import { EmptyState } from "@/components/shared/empty-state";
import { SeverityBadge, TierBadge } from "@/components/shared/status-badge";
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
import { useDiffs } from "@/lib/api/hooks";

// Stub /diffs — full filters, Tier tabs, ReflectedBlock delta,
// replay, and anchor-window visualisation land in Phase 9.8.
export default function DiffsPage() {
  const { data, isLoading, isError, error } = useDiffs({ limit: 50 });
  const items = data?.items ?? [];

  return (
    <div className="space-y-4">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Discrepancies</h1>
        <p className="text-sm text-muted-foreground">
          Cross-source disagreements grouped by metric and severity.
        </p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Recent discrepancies</CardTitle>
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
            <EmptyState title="No discrepancies" description="Sources agree — for now." />
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Severity</TableHead>
                  <TableHead>Tier</TableHead>
                  <TableHead>Metric</TableHead>
                  <TableHead>Block</TableHead>
                  <TableHead>Detected</TableHead>
                  <TableHead>Resolved</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.map((diff) => (
                  <TableRow key={diff.id}>
                    <TableCell>
                      <SeverityBadge value={diff.severity} />
                    </TableCell>
                    <TableCell>
                      <TierBadge value={diff.tier} />
                    </TableCell>
                    <TableCell className="font-mono text-xs">{diff.metric_key}</TableCell>
                    <TableCell className="font-mono text-xs">{diff.block}</TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      {new Date(diff.detected_at).toLocaleString()}
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      {diff.resolved ? "Yes" : "—"}
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
