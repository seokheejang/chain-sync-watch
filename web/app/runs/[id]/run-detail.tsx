"use client";

import { ArrowLeft, X } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { toast } from "sonner";

import { EmptyState } from "@/components/shared/empty-state";
import { SeverityBadge, StatusBadge, TierBadge } from "@/components/shared/status-badge";
import { Badge } from "@/components/ui/badge";
import { Button, buttonVariants } from "@/components/ui/button";
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
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import type { Schemas } from "@/lib/api/client";
import { useCancelRun, useRun, useRunDiffs, useSources } from "@/lib/api/hooks";
import { chainLabel } from "@/lib/chains";

// Runs that are still settlable can be cancelled. "Completed" and
// "failed" are terminal; cancelling those is a no-op that the
// backend rejects anyway, so we hide the button rather than surface
// a preventable 409.
const cancellableStatuses = new Set(["pending", "running"]);

export function RunDetail({ id }: { id: string }) {
  const router = useRouter();
  const run = useRun(id);
  const diffs = useRunDiffs(id);
  const cancel = useCancelRun();
  // Sources that were enabled for this chain at the time the
  // user opens the detail page. The Run itself doesn't pin the
  // source set historically (a limitation we may revisit), but
  // showing "sources currently enabled for chain N" is the most
  // useful proxy for "what this run compared against".
  const sources = useSources(run.data?.chain_id ?? 0);

  if (run.isLoading) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-8 w-72" />
        <Skeleton className="h-32 w-full" />
        <Skeleton className="h-64 w-full" />
      </div>
    );
  }

  if (run.isError || !run.data) {
    return (
      <EmptyState
        title="Run not found"
        description={run.error instanceof Error ? run.error.message : "Unknown error"}
        action={
          <Link href="/runs" className={buttonVariants({ variant: "outline" })}>
            <ArrowLeft className="mr-2 h-4 w-4" /> Back to runs
          </Link>
        }
      />
    );
  }

  const r = run.data;
  const canCancel = cancellableStatuses.has(r.status ?? "");

  const handleCancel = () => {
    cancel.mutate(id, {
      onSuccess: () => toast.success("Cancellation requested"),
      onError: (err) => toast.error(err instanceof Error ? err.message : "Cancel failed"),
    });
  };

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between gap-4">
        <div>
          <Link href="/runs" className={buttonVariants({ variant: "ghost", size: "sm" }) + " mb-2"}>
            <ArrowLeft className="mr-2 h-4 w-4" /> Runs
          </Link>
          <h1 className="font-mono text-lg font-semibold">{r.id}</h1>
          <p className="text-sm text-muted-foreground">
            {chainLabel(r.chain_id)} · {r.strategy_kind} · {r.trigger_kind}
          </p>
        </div>
        <div className="flex items-center gap-2">
          <StatusBadge value={r.status} />
          {canCancel ? (
            <Button variant="outline" size="sm" onClick={handleCancel} disabled={cancel.isPending}>
              <X className="mr-2 h-4 w-4" />
              {cancel.isPending ? "Cancelling…" : "Cancel run"}
            </Button>
          ) : null}
        </div>
      </div>

      {r.summary ? <SummaryCard summary={r.summary} /> : null}

      <Tabs defaultValue="diffs">
        <TabsList>
          <TabsTrigger value="diffs">Discrepancies</TabsTrigger>
          <TabsTrigger value="details">Details</TabsTrigger>
        </TabsList>

        <TabsContent value="diffs" className="mt-4">
          <Card>
            <CardHeader>
              <CardTitle>Discrepancies produced by this run</CardTitle>
            </CardHeader>
            <CardContent>
              {diffs.isLoading ? (
                <Skeleton className="h-24 w-full" />
              ) : diffs.isError ? (
                <EmptyState title="Couldn't load discrepancies" />
              ) : (diffs.data?.items ?? []).length === 0 ? (
                <AllAgreedEmpty
                  status={r.status}
                  metricCount={(r.metrics ?? []).length}
                  sourceCount={(sources.data?.items ?? []).filter((s) => s.enabled).length}
                />
              ) : (
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Severity</TableHead>
                      <TableHead>Tier</TableHead>
                      <TableHead>Metric</TableHead>
                      <TableHead>Block</TableHead>
                      <TableHead>Detected</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {(diffs.data?.items ?? []).map((d) => (
                      <TableRow
                        key={d.id}
                        className="cursor-pointer"
                        onClick={() => router.push(`/diffs/${d.id}`)}
                      >
                        <TableCell>
                          <SeverityBadge value={d.severity} />
                        </TableCell>
                        <TableCell>
                          <TierBadge value={d.tier} />
                        </TableCell>
                        <TableCell className="font-mono text-xs">{d.metric_key}</TableCell>
                        <TableCell className="font-mono text-xs">{d.block}</TableCell>
                        <TableCell className="text-xs text-muted-foreground">
                          {new Date(d.detected_at).toLocaleString()}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              )}
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="details" className="mt-4">
          <Card>
            <CardHeader>
              <CardTitle>Metadata</CardTitle>
            </CardHeader>
            <CardContent className="grid gap-3 text-sm sm:grid-cols-2">
              <Field label="Status" value={<StatusBadge value={r.status} />} />
              <Field label="Chain" value={chainLabel(r.chain_id)} />
              <Field label="Strategy" value={r.strategy_kind} />
              <Field label="Trigger" value={r.trigger_kind} />
              <Field label="Created" value={new Date(r.created_at).toLocaleString()} />
              <Field
                label="Started"
                value={r.started_at ? new Date(r.started_at).toLocaleString() : "—"}
              />
              <Field
                label="Finished"
                value={r.finished_at ? new Date(r.finished_at).toLocaleString() : "—"}
              />
              <Field
                label="Metrics"
                value={
                  <div className="flex flex-wrap gap-1">
                    {(r.metrics ?? []).map((m) => (
                      <Badge key={m} variant="outline" className="font-mono text-xs">
                        {m}
                      </Badge>
                    ))}
                  </div>
                }
              />
              {r.address_plan_kinds && r.address_plan_kinds.length > 0 ? (
                <Field
                  label="Address plans"
                  value={
                    <div className="flex flex-wrap gap-1">
                      {r.address_plan_kinds.map((k) => (
                        <Badge key={k} variant="outline" className="font-mono text-xs">
                          {k}
                        </Badge>
                      ))}
                    </div>
                  }
                />
              ) : null}
              {r.token_plan_kinds && r.token_plan_kinds.length > 0 ? (
                <Field
                  label="Token plans"
                  value={
                    <div className="flex flex-wrap gap-1">
                      {r.token_plan_kinds.map((k) => (
                        <Badge key={k} variant="outline" className="font-mono text-xs">
                          {k}
                        </Badge>
                      ))}
                    </div>
                  }
                />
              ) : null}
              {r.error_message ? (
                <Field
                  label="Error"
                  value={
                    <code className="whitespace-pre-wrap rounded bg-destructive/10 px-2 py-1 text-xs text-destructive">
                      {r.error_message}
                    </code>
                  }
                  fullWidth
                />
              ) : null}
            </CardContent>
          </Card>

          <Card className="mt-4">
            <CardHeader>
              <CardTitle>Sources involved</CardTitle>
            </CardHeader>
            <CardContent>
              {sources.isLoading ? (
                <Skeleton className="h-16 w-full" />
              ) : (sources.data?.items ?? []).filter((s) => s.enabled).length === 0 ? (
                <p className="text-sm text-muted-foreground">
                  No enabled sources for this chain — verification had nothing to compare.
                </p>
              ) : (
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>ID</TableHead>
                      <TableHead>Type</TableHead>
                      <TableHead>Endpoint</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {(sources.data?.items ?? [])
                      .filter((s) => s.enabled)
                      .map((s) => (
                        <TableRow key={s.id}>
                          <TableCell className="font-mono text-xs">{s.id}</TableCell>
                          <TableCell>
                            <Badge variant="outline">{s.type}</Badge>
                          </TableCell>
                          <TableCell className="font-mono text-xs">{s.endpoint}</TableCell>
                        </TableRow>
                      ))}
                  </TableBody>
                </Table>
              )}
              <p className="mt-2 text-xs text-muted-foreground">
                Shows sources currently enabled for chain {r.chain_id}. Runs do not pin a historical
                source set — if a source was added or disabled after this run, the list above
                reflects "now" rather than "when the run executed".
              </p>
            </CardContent>
          </Card>

          {r.summary ? <SubjectsCard summary={r.summary} /> : null}
        </TabsContent>
      </Tabs>
    </div>
  );
}

// SummaryCard sits above the tabs so the "what did this run look at"
// answer is visible regardless of whether the operator is on the
// Discrepancies tab or the Details tab.
function SummaryCard({ summary }: { summary: Schemas["RunSummaryIn"] }) {
  const counts = tallySubjects(summary.subjects ?? []);
  const countsLabel =
    Object.keys(counts).length === 0
      ? "0 subjects"
      : Object.entries(counts)
          .map(([k, n]) => `${n} ${kindLabel(k, n)}`)
          .join(", ");
  return (
    <Card>
      <CardHeader>
        <CardTitle>Execution summary</CardTitle>
      </CardHeader>
      <CardContent className="grid gap-3 text-sm sm:grid-cols-4">
        <Field
          label="Anchor block"
          value={
            summary.anchor_block ? <code className="font-mono">#{summary.anchor_block}</code> : "—"
          }
        />
        <Field label="Comparisons" value={summary.comparisons_count} />
        <Field
          label="Sources"
          value={
            <div className="flex flex-wrap gap-1">
              {(summary.sources_used ?? []).length === 0 ? (
                <span className="text-muted-foreground">—</span>
              ) : (
                (summary.sources_used ?? []).map((s) => (
                  <Badge key={s} variant="outline" className="font-mono text-xs">
                    {s}
                  </Badge>
                ))
              )}
            </div>
          }
        />
        <Field label="Subjects" value={countsLabel} />
      </CardContent>
    </Card>
  );
}

// SubjectsCard renders the full subject list grouped by kind. Log-
// style readout so operators can answer "exactly which blocks /
// addresses / tokens were verified?" at a glance, per the MVP
// audit-trail requirement (runs.subjects JSONB).
function SubjectsCard({ summary }: { summary: Schemas["RunSummaryIn"] }) {
  const groups = groupSubjects(summary.subjects ?? []);
  const kinds = Object.keys(groups);
  return (
    <Card className="mt-4">
      <CardHeader>
        <CardTitle>Subjects compared</CardTitle>
      </CardHeader>
      <CardContent className="space-y-3">
        {kinds.length === 0 ? (
          <p className="text-sm text-muted-foreground">
            No subjects recorded for this run. Legacy rows (predating the summary column) render
            empty.
          </p>
        ) : (
          kinds.map((kind) => (
            <div key={kind} className="space-y-1">
              <div className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
                {kindLabel(kind, groups[kind].length)} ({groups[kind].length})
              </div>
              <div className="flex flex-wrap gap-1">
                {groups[kind].map((s) => (
                  <code
                    key={subjectKey(s)}
                    className="rounded bg-muted px-2 py-0.5 font-mono text-xs"
                  >
                    {renderSubject(s)}
                  </code>
                ))}
              </div>
            </div>
          ))
        )}
      </CardContent>
    </Card>
  );
}

// tallySubjects returns a kind → count histogram. Used in the
// SummaryCard header line to summarise the full subject mix in one
// sentence without dumping every entry.
function tallySubjects(subjects: Schemas["SubjectOut"][]): Record<string, number> {
  const out: Record<string, number> = {};
  for (const s of subjects) {
    out[s.k] = (out[s.k] ?? 0) + 1;
  }
  return out;
}

// groupSubjects buckets by kind, preserving insertion order so the
// log renders in the order the Run executed (block pass first, then
// address, etc.).
function groupSubjects(subjects: Schemas["SubjectOut"][]): Record<string, Schemas["SubjectOut"][]> {
  const out: Record<string, Schemas["SubjectOut"][]> = {};
  for (const s of subjects) {
    if (!out[s.k]) out[s.k] = [];
    out[s.k].push(s);
  }
  return out;
}

// kindLabel renders the backend kind slug into a pluralisable human
// word. Singular/plural chosen by count so "1 block, 2 addresses"
// reads naturally.
function kindLabel(kind: string, count: number): string {
  const plural = count !== 1;
  switch (kind) {
    case "block":
      return plural ? "blocks" : "block";
    case "address_latest":
      return plural ? "addresses @latest" : "address @latest";
    case "address_at_block":
      return plural ? "address × block pairs" : "address × block pair";
    case "erc20_balance":
      return plural ? "erc20 balances" : "erc20 balance";
    case "erc20_holdings":
      return plural ? "erc20 holdings" : "erc20 holding";
    case "snapshot":
      return plural ? "snapshots" : "snapshot";
    default:
      return kind;
  }
}

// subjectKey joins the kind-discriminated fields into a stable React
// key. Duplicates within one Run are theoretically possible (same
// subject compared twice) but never happen in practice — the engine
// de-dupes plans before fan-out.
function subjectKey(s: Schemas["SubjectOut"]): string {
  return `${s.k}|${s.b ?? ""}|${s.a ?? ""}|${s.t ?? ""}|${s.n ?? ""}`;
}

// renderSubject formats one Subject into a readable log line. Short
// hex addresses (first 6 + last 4 chars) keep the chip density
// manageable when a run samples dozens of addresses.
function renderSubject(s: Schemas["SubjectOut"]): string {
  const short = (v: string) => (v.length > 12 ? `${v.slice(0, 6)}…${v.slice(-4)}` : v);
  switch (s.k) {
    case "block":
      return `#${s.b}`;
    case "address_latest":
      return short(s.a ?? "");
    case "address_at_block":
      return `${short(s.a ?? "")}@#${s.b}`;
    case "erc20_balance":
      return `${short(s.a ?? "")} / ${short(s.t ?? "")}`;
    case "erc20_holdings":
      return short(s.a ?? "");
    case "snapshot":
      return s.n || "snapshot";
    default:
      return s.k;
  }
}

// AllAgreedEmpty renders the "0 discrepancies" state as a success
// callout rather than a generic blank slate. The operator sees
// immediately that the run executed against N sources × M metrics
// and every comparison agreed — which IS the happy outcome.
function AllAgreedEmpty({
  status,
  metricCount,
  sourceCount,
}: {
  status: string | undefined;
  metricCount: number;
  sourceCount: number;
}) {
  const terminal = status === "completed" || status === "failed" || status === "cancelled";
  if (!terminal) {
    return (
      <EmptyState
        title="Run still in flight"
        description="Discrepancies surface after the worker finishes each block's comparisons."
      />
    );
  }
  if (status !== "completed") {
    return (
      <EmptyState
        title="No discrepancies recorded"
        description={`Run ended as ${status}. Discrepancies are only persisted for completed runs.`}
      />
    );
  }
  const detail =
    sourceCount > 0
      ? `${sourceCount} source${sourceCount === 1 ? "" : "s"} × ${metricCount} metric${metricCount === 1 ? "" : "s"} — all comparisons agreed.`
      : `${metricCount} metric${metricCount === 1 ? "" : "s"} — no divergence detected.`;
  return (
    <div className="rounded-lg border border-emerald-200 bg-emerald-50 px-6 py-8 text-center dark:border-emerald-900 dark:bg-emerald-950/30">
      <p className="text-sm font-medium text-emerald-900 dark:text-emerald-200">
        ✓ All sources agreed
      </p>
      <p className="mt-1 text-sm text-emerald-800/80 dark:text-emerald-200/80">{detail}</p>
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
