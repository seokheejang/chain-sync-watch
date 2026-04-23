"use client";

import { ArrowLeft, RefreshCw } from "lucide-react";
import Link from "next/link";
import { toast } from "sonner";

import { EmptyState } from "@/components/shared/empty-state";
import { SeverityBadge, TierBadge } from "@/components/shared/status-badge";
import { Badge } from "@/components/ui/badge";
import { Button, buttonVariants } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useDiff, useReplayDiff } from "@/lib/api/hooks";

export function DiffDetail({ id }: { id: string }) {
  const diff = useDiff(id);
  const replay = useReplayDiff();

  if (diff.isLoading) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-8 w-72" />
        <Skeleton className="h-40 w-full" />
        <Skeleton className="h-64 w-full" />
      </div>
    );
  }
  if (diff.isError || !diff.data) {
    return (
      <EmptyState
        title="Discrepancy not found"
        description={diff.error instanceof Error ? diff.error.message : "Unknown error"}
        action={
          <Link href="/diffs" className={buttonVariants({ variant: "outline" })}>
            <ArrowLeft className="mr-2 h-4 w-4" /> Back
          </Link>
        }
      />
    );
  }

  const d = diff.data;
  const values = d.values ?? [];
  // Majority analysis: group raw strings, rank by count, flag any
  // source whose value falls outside the plurality as the
  // "dissenting" side. Operators use this to decide which source
  // to trust / investigate further.
  const valueCounts: Record<string, number> = {};
  for (const v of values) {
    valueCounts[v.raw] = (valueCounts[v.raw] ?? 0) + 1;
  }
  const sortedValues = Object.entries(valueCounts).sort((a, b) => b[1] - a[1]);
  const majorityRaw = sortedValues[0]?.[0];
  const trustedSet = new Set(d.trusted_sources ?? []);

  const handleReplay = () => {
    replay.mutate(id, {
      onSuccess: (res) => {
        if (res?.resolved) {
          toast.success("Replay: sources now agree — diff marked resolved");
        } else if (res?.new_diff_id) {
          toast.warning(`Replay: still disagrees — new diff ${res.new_diff_id}`);
        } else {
          toast.info("Replay complete");
        }
      },
      onError: (err) => toast.error(err instanceof Error ? err.message : "Replay failed"),
    });
  };

  return (
    <div className="space-y-6">
      <div>
        <Link
          href={d.run_id ? `/runs/${d.run_id}` : "/diffs"}
          className={`${buttonVariants({ variant: "ghost", size: "sm" })} mb-2`}
        >
          <ArrowLeft className="mr-2 h-4 w-4" />{" "}
          {d.run_id ? `Run ${d.run_id.slice(0, 8)}…` : "Back"}
        </Link>
        <div className="flex flex-wrap items-center gap-3">
          <h1 className="font-mono text-lg font-semibold">{d.id}</h1>
          <SeverityBadge value={d.severity} />
          <TierBadge value={d.tier} />
          {d.resolved ? (
            <Badge
              variant="outline"
              className="bg-emerald-100 text-emerald-900 dark:bg-emerald-950 dark:text-emerald-200"
            >
              resolved
            </Badge>
          ) : (
            <Button size="sm" variant="outline" onClick={handleReplay} disabled={replay.isPending}>
              <RefreshCw className="mr-2 h-3.5 w-3.5" />
              {replay.isPending ? "Replaying…" : "Replay"}
            </Button>
          )}
        </div>
        <p className="mt-1 text-sm text-muted-foreground">
          {d.metric_category} · {d.metric_key} · block {d.block} · detected{" "}
          {new Date(d.detected_at).toLocaleString()}
        </p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Source-by-source values</CardTitle>
          <CardDescription>
            Raw strings are compared byte-for-byte by the ExactMatch tolerance. Rows marked{" "}
            <Badge variant="outline" className="text-xs">
              minority
            </Badge>{" "}
            hold values that differ from the majority — investigate those first.
          </CardDescription>
        </CardHeader>
        <CardContent>
          {values.length === 0 ? (
            <EmptyState title="No snapshots recorded" />
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Source</TableHead>
                  <TableHead>Raw value</TableHead>
                  <TableHead>Fetched</TableHead>
                  <TableHead>Reflected block</TableHead>
                  <TableHead>Verdict</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {[...values]
                  .sort((a, b) => (a.source_id ?? "").localeCompare(b.source_id ?? ""))
                  .map((v) => {
                    const isMinority = v.raw !== majorityRaw;
                    const isTrusted = trustedSet.has(v.source_id ?? "");
                    return (
                      <TableRow key={v.source_id}>
                        <TableCell className="font-mono text-xs">
                          {v.source_id}
                          {isTrusted ? (
                            <Badge variant="outline" className="ml-2 text-[0.65rem]">
                              trusted
                            </Badge>
                          ) : null}
                        </TableCell>
                        <TableCell className="max-w-lg break-all font-mono text-xs">
                          {v.raw}
                        </TableCell>
                        <TableCell className="text-xs text-muted-foreground">
                          {v.fetched_at ? new Date(v.fetched_at).toLocaleString() : "—"}
                        </TableCell>
                        <TableCell className="text-xs">
                          {v.reflected_block != null ? (
                            <span>
                              {v.reflected_block}
                              {v.reflected_block !== d.block ? (
                                <span className="ml-2 text-muted-foreground">
                                  (Δ {v.reflected_block - d.block})
                                </span>
                              ) : null}
                            </span>
                          ) : (
                            <span className="text-muted-foreground">—</span>
                          )}
                        </TableCell>
                        <TableCell>
                          {isMinority ? (
                            <Badge
                              variant="outline"
                              className="bg-amber-100 text-amber-900 dark:bg-amber-950 dark:text-amber-200"
                            >
                              minority
                            </Badge>
                          ) : (
                            <Badge
                              variant="outline"
                              className="bg-emerald-100 text-emerald-900 dark:bg-emerald-950 dark:text-emerald-200"
                            >
                              majority
                            </Badge>
                          )}
                        </TableCell>
                      </TableRow>
                    );
                  })}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Discrepancy metadata</CardTitle>
        </CardHeader>
        <CardContent className="grid gap-3 text-sm sm:grid-cols-2">
          <Field
            label="Metric key"
            value={<code className="font-mono text-xs">{d.metric_key}</code>}
          />
          <Field label="Category" value={d.metric_category} />
          <Field label="Block" value={<span className="font-mono">{d.block}</span>} />
          <Field
            label="Anchor block"
            value={<span className="font-mono">{d.anchor_block ?? "—"}</span>}
          />
          <Field
            label="Subject"
            value={
              <span className="font-mono text-xs">
                {d.subject?.type}
                {d.subject?.address ? ` · ${d.subject.address}` : ""}
              </span>
            }
          />
          <Field
            label="Sampling seed"
            value={<span className="font-mono text-xs">{d.sampling_seed ?? "—"}</span>}
          />
          {d.resolved ? (
            <Field
              label="Resolved at"
              value={d.resolved_at ? new Date(d.resolved_at).toLocaleString() : "—"}
            />
          ) : null}
          <Field
            label="Run"
            value={
              d.run_id ? (
                <Link
                  href={`/runs/${d.run_id}`}
                  className="font-mono text-xs text-primary underline-offset-4 hover:underline"
                >
                  {d.run_id}
                </Link>
              ) : (
                "—"
              )
            }
            fullWidth
          />
        </CardContent>
      </Card>
    </div>
  );
}

function Field({
  label,
  value,
  fullWidth,
}: {
  label: string;
  value: React.ReactNode;
  fullWidth?: boolean;
}) {
  return (
    <div className={fullWidth ? "sm:col-span-2" : undefined}>
      <div className="text-xs uppercase tracking-wide text-muted-foreground">{label}</div>
      <div className="mt-1">{value}</div>
    </div>
  );
}
