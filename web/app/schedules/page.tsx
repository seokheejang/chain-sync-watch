"use client";

import { EmptyState } from "@/components/shared/empty-state";
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
import { useSchedules } from "@/lib/api/hooks";

// Stub /schedules — the CRUD form (cron-expr picker, plan editors,
// deactivate dialog) lands in Phase 9.9.
export default function SchedulesPage() {
  const { data, isLoading, isError, error } = useSchedules();
  const items = data?.items ?? [];

  return (
    <div className="space-y-4">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Schedules</h1>
        <p className="text-sm text-muted-foreground">
          Recurring verification jobs driven by cron expressions.
        </p>
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
            <EmptyState title="No active schedules" description="Create one via POST /schedules." />
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Job</TableHead>
                  <TableHead>Chain</TableHead>
                  <TableHead>Cron</TableHead>
                  <TableHead>Timezone</TableHead>
                  <TableHead>Active</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.map((s) => (
                  <TableRow key={s.job_id}>
                    <TableCell className="font-mono text-xs">{s.job_id}</TableCell>
                    <TableCell className="font-mono text-xs">{s.chain_id}</TableCell>
                    <TableCell className="font-mono text-xs">{s.cron_expr}</TableCell>
                    <TableCell className="font-mono text-xs">{s.timezone || "UTC"}</TableCell>
                    <TableCell className="text-xs">{s.active ? "Yes" : "No"}</TableCell>
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
